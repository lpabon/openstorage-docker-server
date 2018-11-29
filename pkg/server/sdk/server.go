/*
Package sdk is the gRPC implementation of the SDK gRPC server
Copyright 2018 Portworx

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package sdk

import (
	"fmt"
	"sync"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_auth "github.com/grpc-ecosystem/go-grpc-middleware/auth"

	"github.com/libopenstorage/openstorage/alerts"
	"github.com/libopenstorage/openstorage/api"
	"github.com/libopenstorage/openstorage/api/spec"
	"github.com/libopenstorage/openstorage/cluster"
	"github.com/libopenstorage/openstorage/pkg/auth"
	"github.com/libopenstorage/openstorage/pkg/grpcserver"
	"github.com/libopenstorage/openstorage/volume"
	volumedrivers "github.com/libopenstorage/openstorage/volume/drivers"
)

// AuthenticationType is the types of authentication
type AuthenticationType string

const (
	// Default unix domain socket location
	DefaultUnixDomainSocket = "/var/run/%s.sock"
)

// TLSConfig points to the cert files needed for HTTPS
type TLSConfig struct {
	// CertFile is the path to the cert file
	CertFile string
	// KeyFile is the path to the key file
	KeyFile string
}

// ServerConfig provides the configuration to the SDK server
type ServerConfig struct {
	// Net is the transport for gRPC: unix, tcp, etc.
	// For the gRPC Server. This value goes together with `Address`.
	Net string
	// Address is the port number or the unix domain socket path.
	// For the gRPC Server. This value goes together with `Net`.
	Address string
	// RestAdress is the port number. Example: 9110
	// For the gRPC REST Gateway.
	RestPort string
	// Unix domain socket for local communication. This socket
	// will be used by the REST Gateway to communicate with the gRPC server.
	// Only set for testing. Having a '%s' can be supported to use the
	// name of the driver as the driver name.
	Socket string
	// (optional) The OpenStorage driver to use
	DriverName string
	// (optional) Cluster interface
	Cluster cluster.Cluster
	// AlertsFilterDeleter
	AlertsFilterDeleter alerts.FilterDeleter
	// Authentication configuration
	Auth *auth.JwtAuthConfig
	// Tls configuration
	Tls *TLSConfig
}

// Server is an implementation of the gRPC SDK interface
type Server struct {
	config      ServerConfig
	netServer   *sdkGrpcServer
	udsServer   *sdkGrpcServer
	restGateway *sdkRestGateway
}

type serverAccessor interface {
	alert() alerts.FilterDeleter
	cluster() cluster.Cluster
	driver() volume.VolumeDriver
}

type sdkGrpcServer struct {
	*grpcserver.GrpcServer

	restPort      string
	lock          sync.RWMutex
	name          string
	authenticator auth.Authenticator
	config        ServerConfig
	log           *logrus.Entry

	// Interface implementations
	clusterHandler cluster.Cluster
	driverHandler  volume.VolumeDriver
	alertHandler   alerts.FilterDeleter

	// gRPC Handlers
	clusterServer        *ClusterServer
	nodeServer           *NodeServer
	volumeServer         *VolumeServer
	objectstoreServer    *ObjectstoreServer
	schedulePolicyServer *SchedulePolicyServer
	clusterPairServer    *ClusterPairServer
	cloudBackupServer    *CloudBackupServer
	credentialServer     *CredentialServer
	identityServer       *IdentityServer
	alertsServer         api.OpenStorageAlertsServer
}

// Interface check
var _ grpcserver.Server = &sdkGrpcServer{}

// New creates a new SDK server
func New(config *ServerConfig) (*Server, error) {

	if config == nil {
		return nil, fmt.Errorf("Must provide configuration")
	}

	// Check if the socket is provided to enable the REST gateway to communicate
	// to the unix domain socket
	if len(config.Socket) == 0 {
		return nil, fmt.Errorf("Must provide unix domain socket for SDK")
	}

	// Create a gRPC server on the network
	netServer, err := newSdkGrpcServer(config)
	if err != nil {
		return nil, err
	}

	// Create a gRPC server on a unix domain socket
	udsConfig := *config
	udsConfig.Net = "unix"
	udsConfig.Address = config.Socket
	udsConfig.Tls = nil
	udsServer, err := newSdkGrpcServer(&udsConfig)
	if err != nil {
		return nil, err
	}

	// Create REST Gateway and connect it to the unix domain socket server
	restGeteway, err := newSdkRestGateway(config, udsServer)
	if err != nil {
		return nil, err
	}

	return &Server{
		config:      *config,
		netServer:   netServer,
		udsServer:   udsServer,
		restGateway: restGeteway,
	}, nil
}

// Start all servers
func (s *Server) Start() error {
	if err := s.netServer.Start(); err != nil {
		return err
	} else if err := s.udsServer.Start(); err != nil {
		return err
	}

	if len(s.config.RestPort) != 0 {
		if err := s.restGateway.Start(); err != nil {
			return err
		}
	}

	return nil
}

func (s *Server) Stop() {
	s.netServer.Stop()
	s.udsServer.Stop()
}

func (s *Server) Address() string {
	return s.netServer.Address()
}

func (s *Server) UdsAddress() string {
	return s.udsServer.Address()
}

// UseCluster will setup a new cluster object for the gRPC handlers
func (s *Server) UseCluster(c cluster.Cluster) {
	s.netServer.useCluster(c)
	s.udsServer.useCluster(c)
}

// UseVolumeDriver will setup a new driver object for the gRPC handlers
func (s *Server) UseVolumeDriver(d volume.VolumeDriver) {
	s.netServer.useVolumeDriver(d)
	s.udsServer.useVolumeDriver(d)
}

// UseAlert will setup a new alert object for the gRPC handlers
func (s *Server) UseAlert(a alerts.FilterDeleter) {
	s.netServer.useAlert(a)
	s.udsServer.useAlert(a)
}

// New creates a new SDK gRPC server
func newSdkGrpcServer(config *ServerConfig) (*sdkGrpcServer, error) {
	if nil == config {
		return nil, fmt.Errorf("Configuration must be provided")
	}

	// Create a log object for this server
	name := "SDK-" + config.Net
	log := logrus.WithFields(logrus.Fields{
		"name": name,
	})

	// Save the driver for future calls
	var (
		d   volume.VolumeDriver
		err error
	)
	if len(config.DriverName) != 0 {
		d, err = volumedrivers.Get(config.DriverName)
		if err != nil {
			return nil, fmt.Errorf("Unable to get driver %s info: %s", config.DriverName, err.Error())
		}
	}

	// Setup authentication
	var authenticator auth.Authenticator
	if config.Auth != nil {
		authenticator, err = auth.New(config.Auth)
		if err != nil {
			return nil, err
		}
		log.Info(name + " authentication enabled")
	} else {
		log.Info(name + " authentication disabled")
	}

	// Create gRPC server
	gServer, err := grpcserver.New(&grpcserver.GrpcServerConfig{
		Name:    name,
		Net:     config.Net,
		Address: config.Address,
	})
	if err != nil {
		return nil, fmt.Errorf("Unable to setup %s server: %v", name, err)
	}

	s := &sdkGrpcServer{
		GrpcServer:     gServer,
		config:         *config,
		name:           name,
		log:            log,
		authenticator:  authenticator,
		clusterHandler: config.Cluster,
		driverHandler:  d,
		alertHandler:   config.AlertsFilterDeleter,
	}
	s.identityServer = &IdentityServer{
		server: s,
	}
	s.clusterServer = &ClusterServer{
		server: s,
	}
	s.nodeServer = &NodeServer{
		server: s,
	}
	s.volumeServer = &VolumeServer{
		server:      s,
		specHandler: spec.NewSpecHandler(),
	}
	s.objectstoreServer = &ObjectstoreServer{
		server: s,
	}
	s.schedulePolicyServer = &SchedulePolicyServer{
		server: s,
	}
	s.cloudBackupServer = &CloudBackupServer{
		server: s,
	}
	s.credentialServer = &CredentialServer{
		server: s,
	}
	s.alertsServer = &alertsServer{
		server: s,
	}
	s.clusterPairServer = &ClusterPairServer{
		server: s,
	}

	return s, nil
}

// Start is used to start the server.
// It will return an error if the server is already running.
func (s *sdkGrpcServer) Start() error {

	// Setup https if certs have been provided
	opts := make([]grpc.ServerOption, 0)
	if s.config.Tls != nil {
		creds, err := credentials.NewServerTLSFromFile(s.config.Tls.CertFile, s.config.Tls.KeyFile)
		if err != nil {
			return fmt.Errorf("Failed to create credentials from cert files: %v", err)
		}
		opts = append(opts, grpc.Creds(creds))
		s.log.Info("SDK TLS enabled")
	} else {
		s.log.Info("SDK TLS disabled")
	}

	// Setup authentication and authorization using interceptors if auth is enabled
	if s.config.Auth != nil {
		opts = append(opts, grpc.UnaryInterceptor(
			grpc_middleware.ChainUnaryServer(
				s.rwlockIntercepter,
				grpc_auth.UnaryServerInterceptor(s.auth),
				s.authorizationServerInterceptor,
				s.loggerServerInterceptor,
			)))
	} else {
		opts = append(opts, grpc.UnaryInterceptor(
			grpc_middleware.ChainUnaryServer(
				s.rwlockIntercepter,
				s.loggerServerInterceptor,
			)))
	}

	// Start the gRPC Server
	err := s.GrpcServer.StartWithServer(func() *grpc.Server {
		grpcServer := grpc.NewServer(opts...)

		api.RegisterOpenStorageClusterServer(grpcServer, s.clusterServer)
		api.RegisterOpenStorageNodeServer(grpcServer, s.nodeServer)
		api.RegisterOpenStorageObjectstoreServer(grpcServer, s.objectstoreServer)
		api.RegisterOpenStorageSchedulePolicyServer(grpcServer, s.schedulePolicyServer)
		api.RegisterOpenStorageIdentityServer(grpcServer, s.identityServer)
		api.RegisterOpenStorageVolumeServer(grpcServer, s.volumeServer)
		api.RegisterOpenStorageMigrateServer(grpcServer, s.volumeServer)
		api.RegisterOpenStorageCredentialsServer(grpcServer, s.credentialServer)
		api.RegisterOpenStorageCloudBackupServer(grpcServer, s.cloudBackupServer)
		api.RegisterOpenStorageMountAttachServer(grpcServer, s.volumeServer)
		api.RegisterOpenStorageAlertsServer(grpcServer, s.alertsServer)
		api.RegisterOpenStorageClusterPairServer(grpcServer, s.clusterPairServer)
		return grpcServer
	})
	if err != nil {
		return err
	}

	return nil
}

func (s *sdkGrpcServer) useCluster(c cluster.Cluster) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.clusterHandler = c
}

func (s *sdkGrpcServer) useVolumeDriver(d volume.VolumeDriver) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.driverHandler = d
}

func (s *sdkGrpcServer) useAlert(a alerts.FilterDeleter) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.alertHandler = a
}

// Accessors
func (s *sdkGrpcServer) driver() volume.VolumeDriver {
	return s.driverHandler
}

func (s *sdkGrpcServer) cluster() cluster.Cluster {
	return s.clusterHandler
}

func (s *sdkGrpcServer) alert() alerts.FilterDeleter {
	return s.alertHandler
}
