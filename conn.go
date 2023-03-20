package grpc_conn

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/bredtape/retry"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

var (
	metric_grpc_is_connected = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "grpc_is_connected",
		Help: "Whether the connection to the named service has been etablished"},
		[]string{"service", "address"})

	metric_grpc_conns = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "grpc_connection_attempts_total",
		Help: "Total number of attempts to connect to the named service"},
		[]string{"service", "address"})

	metric_grpc_conns_err = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "grpc_connection_attempts_error",
		Help: "Total number of attempts to connect to the named service, that resulted in some error"},
		[]string{"service", "address"})

	DefaultOptions = Options{
		ConnectTimeout: 15 * time.Second,
		RetryConnect:   retry.Must(retry.NewExp(0.2, 200*time.Millisecond, 5*time.Second)),
		DialOptions: []grpc.DialOption{
			grpc.WithBlock(),
			grpc.WithUnaryInterceptor(grpc_prometheus.UnaryClientInterceptor),
			grpc.WithStreamInterceptor(grpc_prometheus.StreamClientInterceptor)}}

	ErrShutdown = errors.New("shutdown in progress")
)

type Options struct {
	ConnectTimeout time.Duration
	RetryConnect   retry.Retryer
	DialOptions    []grpc.DialOption
}

type Conn struct {
	name    string
	address string
	options Options

	once     sync.Once
	requests chan *grpc.ClientConn
}

// New named gRPC connection with address and optional (0..1) Options. Will default to 'DefaultOptions' is not specified
// Remember to call Start!
func New(name, address string, opts ...Options) (*Conn, error) {
	if len(name) == 0 {
		return nil, errors.New("specify name")
	}

	_, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.Wrap(err, "invalid address")
	}

	if len(opts) > 1 {
		return nil, errors.New("specify 0..1 Options")
	}

	c := &Conn{
		name:     name,
		address:  address,
		requests: make(chan *grpc.ClientConn)}

	if len(opts) == 0 {
		c.options = DefaultOptions
	} else {
		c.options = opts[0]
	}

	return c, nil
}

// start connecting and answer requests
func (c *Conn) Start(ctx context.Context) {
	c.once.Do(func() { c.loop(ctx) })
}

func (c *Conn) GetName() string {
	return c.name
}

func (c *Conn) GetAddress() string {
	return c.address
}

func (c *Conn) GetOptions() Options {
	return c.options
}

// try to obtain connection until the context expires. The *Conn must have been Start'ed
func (c *Conn) GetConnection(ctx context.Context) (*grpc.ClientConn, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case conn, ok := <-c.requests:
		if !ok {
			return nil, ErrShutdown
		}
		return conn, nil
	}
}

func (c *Conn) loop(appCtx context.Context) {
	log := logrus.WithFields(logrus.Fields{
		"context": "gRPC conn",
		"name":    c.name,
		"address": c.address})

	defer close(c.requests)

	labels := []string{c.name, c.address}

	// init labels
	metric_grpc_is_connected.WithLabelValues(labels...)
	metric_grpc_conns.WithLabelValues(labels...)
	metric_grpc_conns_err.WithLabelValues(labels...)

	attempt := 0
	for {
		metric_grpc_conns.WithLabelValues(labels...).Inc()
		log.Trace("dialing")

		ctx, cancel := context.WithTimeout(appCtx, c.options.ConnectTimeout)
		conn, err := grpc.DialContext(ctx, c.address, c.options.DialOptions...)
		cancel()
		if err != nil {
			log.WithError(err).Error("failed to dial")
			metric_grpc_conns_err.WithLabelValues(labels...).Inc()

			select {
			case <-appCtx.Done():
				return
			case <-time.After(c.options.RetryConnect.Next(attempt)):
				attempt++
				continue
			}
		}

		attempt = 0
		log.Trace("connected")
		metric_grpc_is_connected.WithLabelValues(labels...).Set(1)
		for {
			select {
			case <-ctx.Done():
				return
			case c.requests <- conn:
			}
		}
	}
}
