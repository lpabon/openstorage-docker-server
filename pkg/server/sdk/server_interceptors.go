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
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	sdk_auth "github.com/libopenstorage/openstorage-sdk-auth/pkg/auth"
	"github.com/pborman/uuid"
	"github.com/sirupsen/logrus"

	grpc_auth "github.com/grpc-ecosystem/go-grpc-middleware/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type InterceptorContextkey string

const (
	InterceptorContextTokenKey InterceptorContextkey = "tokenclaims"
)

var (
	defaultRoles = map[string][]sdk_auth.Rule{
		"admin": {
			{
				Services: []string{"*"},
				Apis:     []string{"*"},
			},
		},
	}
)

// This interceptor provides a way to lock out any calls while we adjust the server
func (s *sdkGrpcServer) rwlockIntercepter(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return handler(ctx, req)
}

// Authenticate user and add authorization information back in the context
func (s *sdkGrpcServer) auth(ctx context.Context) (context.Context, error) {
	token, err := grpc_auth.AuthFromMD(ctx, "bearer")
	if err != nil {
		return nil, err
	}

	// Authenticate user
	claims, err := s.authenticator.AuthenticateToken(token)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}

	// Add authorization information back into the context so that other
	// functions can get access to this information
	ctx = context.WithValue(ctx, InterceptorContextTokenKey, claims)

	return ctx, nil
}

func (s *sdkGrpcServer) loggerServerInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	reqid := uuid.New()
	logger := logrus.WithFields(logrus.Fields{
		"method": info.FullMethod,
		"reqid":  reqid,
	})

	logger.Info("Start")
	ts := time.Now()
	i, err := handler(ctx, req)
	duration := time.Now().Sub(ts)
	if err != nil {
		logger.WithFields(logrus.Fields{"duration": duration}).Infof("Failed: %v", err)
	} else {
		logger.WithFields(logrus.Fields{"duration": duration}).Info("Successful")
	}

	return i, err
}

func (s *sdkGrpcServer) authorizationServerInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	claims, ok := ctx.Value(InterceptorContextTokenKey).(*sdk_auth.Claims)
	if !ok {
		return nil, status.Errorf(codes.Internal, "Authorization called without token")
	}

	// Setup auditor log
	claimsJSON, _ := json.Marshal(claims)
	logger := logrus.WithFields(logrus.Fields{
		"name":   claims.Name,
		"email":  claims.Email,
		"role":   claims.Role,
		"claims": string(claimsJSON),
		"method": info.FullMethod,
	})

	// Determine rules
	var rules []sdk_auth.Rule
	if len(claims.Rules) != 0 {
		rules = claims.Rules
	} else {
		if len(claims.Role) == 0 {
			return nil, status.Error(codes.PermissionDenied, "Access denied, no roles or rules set")
		}
		rules, ok = defaultRoles[claims.Role]
		if !ok {
			return nil, status.Errorf(
				codes.PermissionDenied,
				"Access denied, unknown role: %s", claims.Role)
		}
	}

	// Authorize
	if err := authorizeClaims(rules, info.FullMethod); err != nil {
		logger.Infof("Access denied")
		return nil, status.Errorf(
			codes.PermissionDenied,
			"Access to %s denied",
			info.FullMethod)
	}

	logger.Info("Authorized")
	return handler(ctx, req)
}

func authorizeClaims(rules []sdk_auth.Rule, fullmethod string) error {

	var (
		reqService, reqApi string
	)

	// String: "/openstorage.api.OpenStorage<service>/<method>"
	parts := strings.Split(fullmethod, "/")

	if len(parts) > 1 {
		reqService = strings.TrimPrefix(strings.ToLower(parts[1]), "openstorage.api.openstorage")
	}

	if len(parts) > 2 {
		reqApi = strings.ToLower(parts[2])
	}

	// Go through each rule until a match is found
	for _, rule := range rules {
		for _, service := range rule.Services {
			if service == "*" ||
				service == reqService {
				for _, api := range rule.Apis {
					if api == "*" ||
						api == reqApi {
						return nil
					}
				}
			}
		}
	}

	return fmt.Errorf("no accessable rule to authorize access found")
}
