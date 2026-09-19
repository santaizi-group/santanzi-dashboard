package rpc

import (
	"crypto/x509"
	"fmt"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"

	"github.com/hi2shark/santaizi-dashboard/model"
	pb "github.com/hi2shark/santaizi-dashboard/proto"
	"github.com/hi2shark/santaizi-dashboard/service/pki"
	rpcService "github.com/hi2shark/santaizi-dashboard/service/rpc"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
)

func ServeRPC(port uint) {
	bundle, err := pki.LoadOrCreate(pki.DefaultDir(singleton.Conf.Telemetry.DataDir))
	if err != nil {
		panic(err)
	}
	options, err := grpcServerOptions(singleton.Conf.GRPCTLS, bundle, false)
	if err != nil {
		panic(err)
	}
	server := grpc.NewServer(options...)
	rpcService.SantaiziHandlerSingleton = rpcService.NewSantaiziHandler()
	pb.RegisterSantaiziServiceServer(server, rpcService.SantaiziHandlerSingleton)
	v2Handler, err := rpcService.NewV2Handler()
	if err != nil {
		panic(err)
	}
	pb.RegisterSantaiziTelemetryServiceServer(server, v2Handler)
	pb.RegisterSantaiziControlServiceServer(server, v2Handler)
	pb.RegisterSantaiziNATServiceServer(server, v2Handler)
	pb.RegisterSantaiziEnrollmentServiceServer(server, rpcService.NewEnrollmentHandler(bundle.Agent))
	collectorHandler, err := rpcService.NewPrimaryCollectorHandler(bundle)
	if err != nil {
		panic(err)
	}
	pb.RegisterSantaiziCollectorServiceServer(server, collectorHandler)
	pb.RegisterSantaiziReplicationServiceServer(server, collectorHandler)
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(server, healthServer)
	listen, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		panic(err)
	}
	_ = server.Serve(listen)
}

func grpcServerOptions(cfg model.GRPCTLSConfig, bundle *pki.Bundle, collectorListener bool) ([]grpc.ServerOption, error) {
	policy := rpcService.DeviceAuthPolicy{
		RequireAgentMTLS:     cfg.RequireAgentMTLS,
		RequireCollectorMTLS: cfg.RequireCollectorMTLS,
		ForceAgentIngest:     collectorListener && cfg.Enabled,
	}
	options := append(grpcKeepaliveOptions(),
		grpc.ChainUnaryInterceptor(rpcService.UnaryDeviceAuth(policy)),
		grpc.ChainStreamInterceptor(rpcService.StreamDeviceAuth(policy)),
	)
	if !cfg.Enabled {
		return options, nil
	}
	tlsOpts := pki.ServerTLSOptions{
		CertFile:     cfg.CertFile,
		KeyFile:      cfg.KeyFile,
		ClientCAFile: cfg.ClientCAFile,
	}
	if bundle != nil {
		if bundle.Agent != nil {
			tlsOpts.AgentCA = bundle.Agent.Cert
		}
		if bundle.Collector != nil {
			tlsOpts.CollectorCA = bundle.Collector.Cert
		}
	}
	creds, err := pki.GRPCServerTLS(tlsOpts)
	if err != nil {
		return nil, err
	}
	return append(options, grpc.Creds(creds)), nil
}

func collectorServerOptions(cfg model.GRPCTLSConfig, agentCAProvider func() *x509.CertPool) ([]grpc.ServerOption, error) {
	policy := rpcService.DeviceAuthPolicy{ForceAgentIngest: cfg.Enabled}
	options := append(grpcKeepaliveOptions(),
		grpc.ChainUnaryInterceptor(rpcService.UnaryDeviceAuth(policy)),
		grpc.ChainStreamInterceptor(rpcService.StreamDeviceAuth(policy)),
	)
	if !cfg.Enabled {
		return options, nil
	}
	creds, err := pki.GRPCServerTLS(pki.ServerTLSOptions{
		CertFile: cfg.CertFile, KeyFile: cfg.KeyFile, ClientCAFile: cfg.ClientCAFile,
		ClientCAsProvider: agentCAProvider,
	})
	if err != nil {
		return nil, err
	}
	return append(options, grpc.Creds(creds)), nil
}

func grpcKeepaliveOptions() []grpc.ServerOption {
	return []grpc.ServerOption{
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             10 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    60 * time.Second,
			Timeout: 20 * time.Second,
		}),
	}
}

func DispatchMonitor(serviceSentinelDispatchBus <-chan model.Monitor) {
	for monitor := range serviceSentinelDispatchBus {
		rpcService.DispatchMonitor(monitor)
	}
}
