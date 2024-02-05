package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"	
)

var (
	totalCached = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "clauganxo",
		Name:      "total_cached",
		Help:      "The total number of cached objects",
	})
	servedCache = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "clauganxo",
		Name:      "served_cache",
		Help:      "Total files served from cache",
	})
	missCache = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "clauganxo",
		Name:      "missed_cache",
		Help:      "Total files missed from cache",
	})
	failedRequests = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "clauganxo",
		Name:      "failed_requests",
		Help:      "Requests that failed to serve",
	})
	cacheResponseTime = promauto.NewSummary(prometheus.SummaryOpts{
		Namespace:  "clauganxo",
		Name:       "cache_response_time",
		Help:       "Duration of the login request.",
		Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
	})
)