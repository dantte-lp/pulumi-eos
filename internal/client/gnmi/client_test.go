package gnmi

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestParsePath_Simple(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in    string
		elems []string
	}{
		{"interfaces", []string{"interfaces"}},
		{"/interfaces", []string{"interfaces"}},
		{"interfaces/interface", []string{"interfaces", "interface"}},
		{"/interfaces/interface", []string{"interfaces", "interface"}},
		{"a/b/c/d", []string{"a", "b", "c", "d"}},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, err := ParsePath(tc.in)
			if err != nil {
				t.Fatalf("ParsePath(%q): %v", tc.in, err)
			}
			elems := got.GetElem()
			if len(elems) != len(tc.elems) {
				t.Fatalf("ParsePath(%q): got %d elems %+v, want %d", tc.in, len(elems), elems, len(tc.elems))
			}
			for i, want := range tc.elems {
				if elems[i].GetName() != want {
					t.Fatalf("ParsePath(%q) elem[%d] = %q, want %q", tc.in, i, elems[i].GetName(), want)
				}
				if len(elems[i].GetKey()) != 0 {
					t.Fatalf("ParsePath(%q) elem[%d] keys = %v, want empty", tc.in, i, elems[i].GetKey())
				}
			}
		})
	}
}

func TestParsePath_Keys(t *testing.T) {
	t.Parallel()
	got, err := ParsePath("/interfaces/interface[name=Ethernet1]/state/counters")
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		name string
		keys map[string]string
	}{
		{"interfaces", nil},
		{"interface", map[string]string{"name": "Ethernet1"}},
		{"state", nil},
		{"counters", nil},
	}
	elems := got.GetElem()
	if len(elems) != len(want) {
		t.Fatalf("got %d elems, want %d (%+v)", len(elems), len(want), elems)
	}
	for i, w := range want {
		if elems[i].GetName() != w.name {
			t.Fatalf("elem[%d] name = %q, want %q", i, elems[i].GetName(), w.name)
		}
		if w.keys == nil {
			if len(elems[i].GetKey()) != 0 {
				t.Fatalf("elem[%d] keys = %v, want empty", i, elems[i].GetKey())
			}
			continue
		}
		if !reflect.DeepEqual(elems[i].GetKey(), w.keys) {
			t.Fatalf("elem[%d] keys = %v, want %v", i, elems[i].GetKey(), w.keys)
		}
	}
}

func TestParsePath_MultipleKeys(t *testing.T) {
	t.Parallel()
	got, err := ParsePath("network-instances/network-instance[name=DEFAULT]/protocols/protocol[identifier=BGP][name=default]")
	if err != nil {
		t.Fatal(err)
	}
	elems := got.GetElem()
	if elems[3].GetName() != "protocol" {
		t.Fatalf("name = %q, want protocol", elems[3].GetName())
	}
	wantKeys := map[string]string{"identifier": "BGP", "name": "default"}
	if !reflect.DeepEqual(elems[3].GetKey(), wantKeys) {
		t.Fatalf("keys = %v, want %v", elems[3].GetKey(), wantKeys)
	}
}

func TestParsePath_KeyValueWithSlash(t *testing.T) {
	t.Parallel()
	got, err := ParsePath("/system/foo[path=a/b/c]/bar")
	if err != nil {
		t.Fatal(err)
	}
	elems := got.GetElem()
	if len(elems) != 3 {
		t.Fatalf("got %d elems, want 3 (%+v)", len(elems), elems)
	}
	if elems[1].GetKey()["path"] != "a/b/c" {
		t.Fatalf("key path = %q, want a/b/c", elems[1].GetKey()["path"])
	}
}

func TestParsePath_Empty(t *testing.T) {
	t.Parallel()
	if _, err := ParsePath(""); !errors.Is(err, ErrEmptyPathString) {
		t.Fatalf("ParsePath(\"\"): %v, want sentinel ErrEmptyPathString", err)
	}
	got, err := ParsePath("/")
	if err != nil {
		t.Fatalf("ParsePath(\"/\"): %v", err)
	}
	if len(got.GetElem()) != 0 {
		t.Fatalf("ParsePath(\"/\") elems = %+v, want empty", got.GetElem())
	}
}

func TestDial_Validates(t *testing.T) {
	t.Parallel()
	if _, err := Dial(context.Background(), Config{}); !errors.Is(err, ErrHostRequired) {
		t.Fatalf("Dial empty cfg: %v, want sentinel ErrHostRequired", err)
	}
}

func TestDial_NewClient(t *testing.T) {
	t.Parallel()
	cli, err := Dial(context.Background(), Config{
		Host:           "127.0.0.1",
		Port:           1, // unreachable, but Dial is lazy: NewClient does not actually connect.
		PlaintextNoTLS: true,
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() {
		_ = cli.Close()
	}()
	if cli == nil {
		t.Fatal("Dial returned nil client")
	}
	if cli.conn == nil || cli.stub == nil {
		t.Fatal("Client missing conn/stub")
	}
}

func TestClient_RPC_Guards(t *testing.T) {
	t.Parallel()
	var nilCli *Client
	if _, err := nilCli.Capabilities(context.Background()); !errors.Is(err, ErrClientNotInit) {
		t.Fatalf("nil Capabilities: %v, want sentinel ErrClientNotInit", err)
	}
	if _, err := nilCli.Get(context.Background(), []string{"x"}); !errors.Is(err, ErrClientNotInit) {
		t.Fatalf("nil Get: %v, want sentinel ErrClientNotInit", err)
	}
}

func TestClient_Get_RejectsEmptyPaths(t *testing.T) {
	t.Parallel()
	cli, err := Dial(context.Background(), Config{Host: "127.0.0.1", PlaintextNoTLS: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cli.Close()
	}()
	if _, err := cli.Get(context.Background(), nil); !errors.Is(err, ErrEmptyPathString) {
		t.Fatalf("Get(nil): %v, want sentinel ErrEmptyPathString", err)
	}
}

func TestTransportCreds_BadCABundle(t *testing.T) {
	t.Parallel()
	_, err := transportCreds(Config{CACert: []byte("not a pem")})
	if !errors.Is(err, ErrInvalidCABundle) {
		t.Fatalf("got %v, want sentinel ErrInvalidCABundle", err)
	}
}

func TestClose_Idempotent(t *testing.T) {
	t.Parallel()
	var nilCli *Client
	if err := nilCli.Close(); err != nil {
		t.Fatalf("Close on nil: %v", err)
	}
	cli, err := Dial(context.Background(), Config{Host: "127.0.0.1", PlaintextNoTLS: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := cli.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := cli.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
