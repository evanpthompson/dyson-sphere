package observability

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
)

// sdkResource aliases the SDK type so tracing.go reads cleanly without
// importing the resource package twice under different names.
type sdkResource = resource.Resource

// resourceFromAttrs merges our service identity over the SDK defaults, which
// already contribute things like telemetry.sdk.* and any OTEL_RESOURCE_ATTRIBUTES
// set in the environment.
func resourceFromAttrs(attrs ...attribute.KeyValue) (*sdkResource, error) {
	return resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(resource.Default().SchemaURL(), attrs...),
	)
}
