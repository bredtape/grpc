package grpc_conn

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	labelKeys                = []string{"service", "address"}
	metric_grpc_is_connected = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "grpc_is_connected",
		Help: "Whether the connection to the named service has been etablished"},
		labelKeys)

	metric_conn_state = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "grpc_connection_state",
		Help: "Actual connection state (Idle=0, Connecting=1, Ready=2, TransientFailure=3, Shutdown=4). Enumeration from google.golang.org/grpc/connectivity/connectivity.go"},
		labelKeys)

	metric_grpc_conns = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "grpc_connection_attempts_total",
		Help: "Total number of attempts to connect to the named service"},
		labelKeys)

	metric_grpc_conns_err = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "grpc_connection_attempts_error",
		Help: "Total number of attempts to connect to the named service, that resulted in some error"},
		labelKeys)
)
