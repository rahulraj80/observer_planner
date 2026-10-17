package vendorsurface

import (
	"strings"
	"testing"
)

// TestCustomerHostRowDeclaresNoHost (lane ENTAPI-L16C, VCONN-HOST-1): a
// customer_host row may declare no host of its own, and a built one is exempt
// from "declares at least one host tuple".
func TestCustomerHostRowDeclaresNoHost(t *testing.T) {
	ok := Surface{ID: "x.customer", CustomerHost: true}
	if errs := validateCustomerHost(ok); len(errs) != 0 {
		t.Fatalf("hostless customer_host row refused: %v", errs)
	}
	for name, s := range map[string]Surface{
		"api host":   {ID: "x.a", CustomerHost: true, Hosts: []HostTuple{{Scheme: "https", Host: "api.x.example", Port: 443}}},
		"token host": {ID: "x.b", CustomerHost: true, TokenHosts: []HostTuple{{Scheme: "https", Host: "t.x.example", Port: 443}}},
		"template":   {ID: "x.c", CustomerHost: true, HostTemplates: []HostTemplate{{}}},
	} {
		errs := validateCustomerHost(s)
		if len(errs) != 1 || !strings.Contains(errs[0].Error(), "customer_host") {
			t.Errorf("%s: %v, want the customer_host refusal", name, errs)
		}
	}
	built := Surface{ID: "x.built", CustomerHost: true, Reach: ReachPullAPI}
	if !builtRules[0].ok(built) {
		t.Error("a built customer_host pull row must not need a catalog host")
	}
	if builtRules[0].ok(Surface{ID: "x.plain", Reach: ReachPullAPI}) {
		t.Error("a built catalog-host pull row still needs a host")
	}
}
