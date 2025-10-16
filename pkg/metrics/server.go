package metrics

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/sync/errgroup"
	"k8s.io/klog/v2"
)

func Run(ctx context.Context, metricsAddr string) error {
	if metricsAddr == "" {
		return errors.New("metrics address is empty")
	}

	klog.Infof("starting prometheus listener on address %s", metricsAddr)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	serv := &http.Server{
		Addr:              metricsAddr,
		Handler:           mux,
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      5 * time.Second,
	}
	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		if err := serv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})

	g.Go(func() error {
		<-gCtx.Done()
		klog.Info("shutdown prometheus listener")
		return serv.Shutdown(gCtx)
	})

	return g.Wait()
}
