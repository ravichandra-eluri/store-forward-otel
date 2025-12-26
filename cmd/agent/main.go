package main

import (
	"context"
	"flag"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	"github.com/ravichandra-eluri/store-forward-otel/forwarder"
)

func main() {
	var (
		listenAddr    = flag.String("listen", ":4317", "OTLP gRPC listen address")
		endpoint      = flag.String("endpoint", "localhost:14317", "Upstream OTLP gRPC endpoint")
		bufferDir     = flag.String("buffer-dir", "/var/saf/buffer", "Disk buffer directory")
		maxBufferMB   = flag.Int64("max-buffer-mb", 512, "Max buffer size in MB")
		retryInterval = flag.Duration("retry-interval", 30*time.Second, "Interval between flush retries")
		flushTimeout  = flag.Duration("flush-timeout", 10*time.Second, "Timeout per flush attempt")
	)
	flag.Parse()

	log, _ := zap.NewProduction()
	defer log.Sync()

	fwd, err := forwarder.New(forwarder.Config{
		Endpoint:        *endpoint,
		BufferDir:       *bufferDir,
		MaxBufferSizeMB: *maxBufferMB,
		RetryInterval:   *retryInterval,
		FlushTimeout:    *flushTimeout,
	}, log)
	if err != nil {
		log.Fatal("failed to initialise forwarder", zap.Error(err))
	}
	defer fwd.Close()

	srv := grpc.NewServer()
	collectortrace.RegisterTraceServiceServer(srv, fwd)

	lis, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatal("failed to listen", zap.Error(err), zap.String("addr", *listenAddr))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Info("store-forward-otel agent started",
			zap.String("listen", *listenAddr),
			zap.String("upstream", *endpoint),
			zap.String("buffer-dir", *bufferDir),
		)
		if err := srv.Serve(lis); err != nil {
			log.Error("gRPC server error", zap.Error(err))
		}
	}()

	go fwd.Run(ctx)

	<-ctx.Done()
	log.Info("shutting down gracefully")
	srv.GracefulStop()
}
