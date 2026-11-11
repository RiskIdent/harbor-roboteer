package metrics

import (
	appversion "github.com/RiskIdent/harbor-roboteer/internal/version"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/collectors/version"
)

type Registry struct {
	*prometheus.Registry
}

func NewRegistry() *Registry {
	reg := prometheus.NewRegistry()

	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		version.NewCollector("harbor_roboteer", version.WithExtraConstLabels(prometheus.Labels{"version": appversion.Get()})),
	)

	return &Registry{reg}
}
