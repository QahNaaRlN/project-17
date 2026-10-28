package agents

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// STU-600: requests by outcome; tokens only when the provider reported usage.
func TestRecordLLM(t *testing.T) {
	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	recordLLM(ctx, "test/model", Usage{Prompt: 10, Completion: 3}, nil, false)
	recordLLM(ctx, "test/model", Usage{}, ErrUsageUnknown, true)
	recordLLM(ctx, "test/model", Usage{Prompt: 1}, errors.New("provider"), false)
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatal(err)
	}
	totals := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			for _, p := range m.Data.(metricdata.Sum[int64]).DataPoints {
				for _, kv := range p.Attributes.ToSlice() {
					if kv.Key == "gen_ai.token.type" || kv.Key == "cms.outcome" {
						totals[m.Name+":"+kv.Value.AsString()] += p.Value
					}
				}
			}
		}
	}
	want := map[string]int64{"cms.llm.tokens:input": 11, "cms.llm.tokens:output": 3, "cms.llm.requests:ok": 1, "cms.llm.requests:usage_unknown": 1, "cms.llm.requests:provider_error": 1}
	for k, v := range want {
		if totals[k] != v {
			t.Fatal(totals)
		}
	}
}
