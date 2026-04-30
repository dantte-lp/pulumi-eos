// Package device exposes Pulumi resources whose token starts with
// `eos:device:`. It is the canary family used to verify provider wiring.
package device

import (
	"context"
	"errors"
	"fmt"

	"github.com/pulumi/pulumi-go-provider/infer"
)

// Device is a read-only handle representing an Arista EOS switch reachable via
// eAPI or gNMI. It exposes facts (model, serial, EOS version) without ever
// mutating device state.
//
// In S5 onwards, additional `eos:device:*` resources (Configlet, RawCli,
// OsImage, Reboot, Certificate) reuse the same connection-config shape.
type Device struct{}

// DeviceArgs is the set of inputs accepted by the resource.
type DeviceArgs struct {
	// Host is the management address of the EOS switch (DNS or IP).
	Host string `pulumi:"host"`
	// Port overrides the transport's default port (eAPI: 443, gNMI: 6030).
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

// ErrFactsNotWired is returned by readFacts until S5 connects goeapi.
var ErrFactsNotWired = errors.New("device fact gathering not yet wired (S5)")

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

// Read refreshes facts; idempotent. Returning a non-nil error ID-clears the
// resource from the Pulumi state.
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

// readFacts is the canary stub. S5 swaps it for a goeapi `show version` call.
func readFacts(_ context.Context, _ *DeviceState) error {
	return ErrFactsNotWired
}
