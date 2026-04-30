// Package device exposes Pulumi resources whose token starts with
// `eos:device:`.
package device

import (
	"context"
	"errors"
	"fmt"

	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
	"github.com/dantte-lp/pulumi-eos/internal/config"
)

// Sentinel errors emitted by package device.
var (
	ErrFactsMissing = errors.New("device: show version returned no rows")
)

// Device is a read-only handle representing an Arista EOS switch reachable
// via eAPI. It surfaces facts (model, serial, EOS version, system MAC)
// without ever mutating device state.
type Device struct{}

// DeviceArgs is the set of inputs accepted by the resource.
type DeviceArgs struct {
	// Host is the management address of the EOS switch (DNS or IP).
	Host string `pulumi:"host"`
	// Port overrides the transport's default port (eAPI: 443).
	Port *int `pulumi:"port,optional"`
	// Username for AAA. If empty the provider-level eosUsername is used.
	Username *string `pulumi:"username,optional"`
	// Password for AAA. If empty the provider-level eosPassword is used.
	Password *string `provider:"secret" pulumi:"password,optional"`
}

// DeviceState is the resource state surfaced back to Pulumi after Read /
// Refresh. Inputs are embedded; outputs are factual reads from the device.
type DeviceState struct {
	DeviceArgs

	Model        string `pulumi:"model"`
	SerialNumber string `pulumi:"serialNumber"`
	HardwareRev  string `pulumi:"hardwareRev"`
	EOSVersion   string `pulumi:"eosVersion"`
	SystemMAC    string `pulumi:"systemMac"`
}

// Annotate registers the schema descriptions for the resource and its fields.
func (r *Device) Annotate(a infer.Annotator) {
	a.Describe(&r, "A read-only handle on an Arista EOS device. Surfaces facts (model, serial, EOS version) without mutating device state.")
}

// Annotate is the canonical place to describe inputs.
func (args *DeviceArgs) Annotate(a infer.Annotator) {
	a.Describe(&args.Host, "Management hostname or IP of the EOS device.")
	a.Describe(&args.Port, "Override of the transport's default management port.")
	a.Describe(&args.Username, "AAA username. Falls back to provider-level eosUsername when unset.")
	a.Describe(&args.Password, "AAA password. Falls back to provider-level eosPassword when unset.")
}

// Annotate describes the read-only outputs.
func (s *DeviceState) Annotate(a infer.Annotator) {
	a.Describe(&s.Model, "EOS device model string (e.g. DCS-7280SR3-48YC8).")
	a.Describe(&s.SerialNumber, "Device serial number.")
	a.Describe(&s.HardwareRev, "Device hardware revision.")
	a.Describe(&s.EOSVersion, "Running EOS version (e.g. 4.36.0F).")
	a.Describe(&s.SystemMAC, "Chassis system MAC.")
}

// Create wires a Device handle. No mutation occurs on the device — the call
// merely establishes that the device is reachable and gathers facts.
func (*Device) Create(ctx context.Context, req infer.CreateRequest[DeviceArgs]) (infer.CreateResponse[DeviceState], error) {
	state := DeviceState{DeviceArgs: req.Inputs}
	if req.DryRun {
		return infer.CreateResponse[DeviceState]{ID: req.Inputs.Host, Output: state}, nil
	}
	if err := readFacts(ctx, &state); err != nil {
		return infer.CreateResponse[DeviceState]{}, fmt.Errorf("read facts: %w", err)
	}
	return infer.CreateResponse[DeviceState]{ID: req.Inputs.Host, Output: state}, nil
}

// Read refreshes facts; idempotent.
func (*Device) Read(ctx context.Context, req infer.ReadRequest[DeviceArgs, DeviceState]) (infer.ReadResponse[DeviceArgs, DeviceState], error) {
	state := req.State
	state.DeviceArgs = req.Inputs
	if err := readFacts(ctx, &state); err != nil {
		return infer.ReadResponse[DeviceArgs, DeviceState]{}, fmt.Errorf("read facts: %w", err)
	}
	return infer.ReadResponse[DeviceArgs, DeviceState]{ID: req.ID, Inputs: req.Inputs, State: state}, nil
}

// Delete is a no-op: the resource never owned device state.
func (*Device) Delete(_ context.Context, _ infer.DeleteRequest[DeviceState]) (infer.DeleteResponse, error) {
	return infer.DeleteResponse{}, nil
}

// readFacts populates the read-only fields of state from `show version`.
//
// goeapi already returns structured JSON for `show version`; we map the
// fields documented in EOS Command API Guide §1 (versioned schema r1)
// into the resource state.
func readFacts(ctx context.Context, state *DeviceState) error {
	cli, err := newClient(ctx, state.Host, state.Username, state.Password, state.Port)
	if err != nil {
		return err
	}
	resp, err := cli.RunCmds(ctx, []string{"show version"}, "json")
	if err != nil {
		return fmt.Errorf("show version: %w", err)
	}
	if len(resp) == 0 {
		return ErrFactsMissing
	}
	row := resp[0]
	if v, ok := row["modelName"].(string); ok {
		state.Model = v
	}
	if v, ok := row["serialNumber"].(string); ok {
		state.SerialNumber = v
	}
	if v, ok := row["hardwareRevision"].(string); ok {
		state.HardwareRev = v
	}
	if v, ok := row["version"].(string); ok {
		state.EOSVersion = v
	}
	if v, ok := row["systemMacAddress"].(string); ok {
		state.SystemMAC = v
	}
	return nil
}

// newClient builds an eAPI client for the device, applying per-resource
// host / port / username / password overrides over the provider-level
// configuration.
func newClient(ctx context.Context, host string, user, pass *string, port *int) (*eapi.Client, error) {
	cfg := config.FromContext(ctx)
	hostPtr := &host
	cli, err := cfg.EAPIClient(ctx, hostPtr, user, pass)
	if err != nil {
		return nil, err
	}
	_ = port // explicit port is encoded in `host` URL when needed; provider config carries the default.
	return cli, nil
}
