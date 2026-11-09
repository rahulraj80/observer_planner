package otelident

// Echo marker: Observer stamps EchoAttr=EchoValue on the telemetry it
// exports itself (internal/exporter/otel); a receiver that sees it never
// reads the resource back in.
const (
	EchoAttr  = "sbo.emitted_by"
	EchoValue = "observer"
)

// TenantKeys are the attributes a hosted application's tenant is read from,
// in Plane A's candidate order (internal/obs/ingest).
var TenantKeys = []string{"sbo.tenant", "tenant.id", "tenant"}

// UserKeys are the attributes a hosted application's END USER is read from,
// in Plane A's candidate order (internal/obs/ingest).
var UserKeys = []string{"sbo.user", "enduser.id", "user.id", "user"}

// HostedAppKeys returns every hosted-app identity key: TenantKeys then
// UserKeys, as a fresh slice. Any one of them on a resource or record marks
// it a hosted application's (Plane A).
func HostedAppKeys() []string {
	out := make([]string, 0, len(TenantKeys)+len(UserKeys))
	out = append(out, TenantKeys...)
	return append(out, UserKeys...)
}
