package grpcserver

import (
	"context"
	"fmt"
	"net"

	"github.com/lockforge/lockforge/internal/events"
	"github.com/lockforge/lockforge/internal/lockmanager"
	pb "github.com/lockforge/lockforge/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

// Server implements the LockService gRPC server and health check.
type Server struct {
	pb.UnimplementedLockServiceServer
	healthpb.UnimplementedHealthServer
	lm         *lockmanager.LockManager
	events     *events.RingBuffer
	grpcServer *grpc.Server
}

// NewServer creates a new gRPC Server instance.
func NewServer(lm *lockmanager.LockManager, eventBuffer *events.RingBuffer) *Server {
	s := &Server{
		lm:     lm,
		events: eventBuffer,
	}

	grpcServer := grpc.NewServer()
	pb.RegisterLockServiceServer(grpcServer, s)
	healthpb.RegisterHealthServer(grpcServer, s)
	s.grpcServer = grpcServer

	return s
}

// Start listens on the specified port.
func (s *Server) Start(port int) error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", port, err)
	}
	return s.grpcServer.Serve(lis)
}

// Stop gracefully stops the gRPC server.
func (s *Server) Stop() {
	s.grpcServer.GracefulStop()
}

// AcquireLock handles lock acquisition requests.
func (s *Server) AcquireLock(ctx context.Context, req *pb.AcquireLockRequest) (*pb.AcquireLockResponse, error) {
	if req.ResourceId == "" || req.ClientId == "" {
		return nil, status.Error(codes.InvalidArgument, "resource_id and client_id are required")
	}
	return s.lm.AcquireLock(ctx, req.ResourceId, req.ClientId, req.TtlSeconds)
}

// ReleaseLock handles lock release requests.
func (s *Server) ReleaseLock(ctx context.Context, req *pb.ReleaseLockRequest) (*pb.ReleaseLockResponse, error) {
	if req.ResourceId == "" {
		return nil, status.Error(codes.InvalidArgument, "resource_id is required")
	}
	return s.lm.ReleaseLock(ctx, req.ResourceId, req.ClientId, req.LeaseId, req.FencingToken)
}

// RenewLease handles lease renewal keep-alive requests.
func (s *Server) RenewLease(ctx context.Context, req *pb.RenewLeaseRequest) (*pb.RenewLeaseResponse, error) {
	if req.ResourceId == "" {
		return nil, status.Error(codes.InvalidArgument, "resource_id is required")
	}
	return s.lm.RenewLease(ctx, req.ResourceId, req.ClientId, req.LeaseId, req.TtlSeconds)
}

// InspectLock returns current state of a lock.
func (s *Server) InspectLock(ctx context.Context, req *pb.InspectLockRequest) (*pb.InspectLockResponse, error) {
	if req.ResourceId == "" {
		return nil, status.Error(codes.InvalidArgument, "resource_id is required")
	}
	return s.lm.InspectLock(ctx, req.ResourceId)
}

// GetActiveLocks returns all active locks.
func (s *Server) GetActiveLocks(ctx context.Context, req *pb.GetActiveLocksRequest) (*pb.GetActiveLocksResponse, error) {
	locks, err := s.lm.GetActiveLocks(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get active locks: %v", err)
	}
	return &pb.GetActiveLocksResponse{Locks: locks}, nil
}

// StreamUpdates streams real-time lock events over gRPC.
func (s *Server) StreamUpdates(req *pb.StreamUpdatesRequest, stream pb.LockService_StreamUpdatesServer) error {
	ch, unsubscribe := s.events.Subscribe()
	defer unsubscribe()

	ctx := stream.Context()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.Send(ev); err != nil {
				return err
			}
		}
	}
}

// GetSystemEvents returns recent audit events.
func (s *Server) GetSystemEvents(ctx context.Context, req *pb.GetSystemEventsRequest) (*pb.GetSystemEventsResponse, error) {
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 50
	}
	evs := s.events.GetRecent(limit)
	return &pb.GetSystemEventsResponse{Events: evs}, nil
}

// Health check Check endpoint
func (s *Server) Check(ctx context.Context, req *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	return &healthpb.HealthCheckResponse{
		Status: healthpb.HealthCheckResponse_SERVING,
	}, nil
}

// Health check Watch endpoint
func (s *Server) Watch(req *healthpb.HealthCheckRequest, server healthpb.Health_WatchServer) error {
	return server.Send(&healthpb.HealthCheckResponse{
		Status: healthpb.HealthCheckResponse_SERVING,
	})
}
