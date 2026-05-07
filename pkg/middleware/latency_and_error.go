package middleware

import (
	"net/http"
	"time"
)

func CreateLatencyAndErrorMiddleware(params *Params) func(http.Handler) http.Handler {
	log := params.Logger("latency-error")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			reqLog := RequestLog(log, req)
			cfg := params.GetServiceConfig(req)
			endpointPath := getEndpointPath(req, cfg.Name)

			var latency time.Duration
			var errorCode int

			if ep := cfg.GetEndpointConfig(endpointPath, req.Method); ep != nil {
				latency = ep.GetLatency()
				errorCode = ep.GetError()
			} else {
				latency = cfg.GetLatency()
				errorCode = cfg.GetError()
			}

			if latency > 0 {
				reqLog.Info("Latency", "delay", latency)
				select {
				case <-time.After(latency):
				case <-req.Context().Done():
					return
				}
			}

			if errorCode > 0 {
				reqLog.Info("Simulated error", "code", errorCode)
				SetRequestIDHeader(w, req)
				SetDurationHeader(w, req)
				w.Header().Set(ResponseHeaderSource, ResponseHeaderSourceGenerated)
				http.Error(w, "Simulated error", errorCode)
				return
			}

			next.ServeHTTP(w, req)
		})
	}
}
