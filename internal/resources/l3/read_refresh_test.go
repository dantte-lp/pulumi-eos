package l3

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	provider "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/property"

	"github.com/dantte-lp/pulumi-eos/internal/config"
)

func TestReadRefreshResponses(t *testing.T) {
	t.Parallel()
	resources := []struct {
		name, id, transcript, field, value string
		inputs                             map[string]property.Value
	}{
		{
			name: "Rpki", id: "rpki/65000/C1",
			transcript: "router bgp 65000\n   rpki cache C1\n      host 192.0.2.1\n",
			field:      "cacheHost", value: "192.0.2.1",
			inputs: map[string]property.Value{
				"name": property.New("C1"), "bgpAsn": property.New(float64(65000)),
				"cacheHost": property.New("192.0.2.2"),
			},
		},
		{
			name: "Vrrp", id: "vrrp/Vlan100/1",
			transcript: "interface Vlan100\n   vrrp 1 description refreshed\n",
			field:      "description", value: "refreshed",
			inputs: map[string]property.Value{
				"interface": property.New("Vlan100"), "vrid": property.New(float64(1)),
			},
		},
	}
	for _, res := range resources {
		for _, mode := range []string{"found", "missing", "read_error", "client_error"} {
			t.Run(res.name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var request struct {
						ID     any `json:"id"`
						Params struct {
							Commands []string `json:"cmds"`
						} `json:"params"`
					}
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Errorf("decode request: %v", err)
						return
					}
					commands := request.Params.Commands
					versionRequest := len(commands) > 0 && commands[len(commands)-1] == "show version"
					if mode == "read_error" && !versionRequest {
						http.Error(w, "fixture failure", http.StatusInternalServerError)
						return
					}
					output := res.transcript
					if mode == "missing" {
						output = ""
					}
					result := map[string]string{"output": output}
					if versionRequest {
						result = map[string]string{"version": "fixture"}
					}
					results := make([]map[string]string, len(commands))
					for i := range results {
						results[i] = map[string]string{}
					}
					if len(results) > 0 {
						results[len(results)-1] = result
					}
					w.Header().Set("Content-Type", "application/json")
					err := json.NewEncoder(w).Encode(map[string]any{
						"jsonrpc": "2.0", "id": request.ID,
						"result": results,
					})
					if err != nil {
						t.Errorf("encode response: %v", err)
					}
				}))
				defer srv.Close()
				cfg := config.Config{EOSURL: srv.URL, EOSUsername: "fixture"}
				if mode == "client_error" {
					cfg.EOSURL = ""
				}
				p, err := infer.NewProviderBuilder().WithNamespace("eos").
					WithConfig(infer.Config(&cfg)).
					WithResources(infer.Resource(&Rpki{}), infer.Resource(&Vrrp{})).Build()
				if err != nil {
					t.Fatalf("build provider: %v", err)
				}
				inputs := property.NewMap(res.inputs)
				resp, err := p.Read(t.Context(), provider.ReadRequest{
					ID: res.id, Urn: resource.URN("urn:pulumi:test::fixture::eos:l3:" + res.name + "::sample"),
					Inputs: inputs, Properties: inputs,
				})
				if mode == "read_error" || mode == "client_error" {
					if err == nil {
						t.Fatal("Read succeeded despite fixture error")
					}
					return
				}
				if err != nil {
					t.Fatalf("Read: %v", err)
				}
				if mode == "missing" {
					if resp.ID != "" {
						t.Fatalf("missing resource ID = %q", resp.ID)
					}
					return
				}
				if resp.ID != res.id || resp.Properties.Get(res.field).AsString() != res.value {
					t.Fatalf("Read returned unexpected ID or refreshed property: %+v", resp)
				}
				if !resp.Inputs.Equals(inputs) {
					t.Fatalf("Read changed inputs: %+v", resp.Inputs)
				}
			})
		}
	}
}
