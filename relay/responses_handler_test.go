package relay

import (
	"bytes"
	"testing"

	"github.com/tidwall/gjson"
)

func TestRewritePassThroughResponsesModel(t *testing.T) {
	body := []byte(`{"model":"public-model","input":[{"role":"user","content":"hello"}],"metadata":{"model":"nested-model","large":9007199254740993},"unknown":true}`)

	rewritten, err := rewritePassThroughResponsesModel(body, " provider-model ")
	if err != nil {
		t.Fatalf("rewritePassThroughResponsesModel() error = %v", err)
	}
	if got := gjson.GetBytes(rewritten, "model").String(); got != "provider-model" {
		t.Fatalf("top-level model = %q, want %q", got, "provider-model")
	}
	if got := gjson.GetBytes(rewritten, "metadata.model").String(); got != "nested-model" {
		t.Fatalf("nested model = %q, want unchanged", got)
	}
	if got := gjson.GetBytes(rewritten, "metadata.large").Raw; got != "9007199254740993" {
		t.Fatalf("large integer = %q, want exact value preserved", got)
	}
	if !gjson.GetBytes(rewritten, "unknown").Bool() {
		t.Fatal("unknown field was not preserved")
	}
}

func TestRewritePassThroughResponsesModelWithoutUpstreamModel(t *testing.T) {
	body := []byte(`{ "model": "public-model", "unknown": 1 }`)

	rewritten, err := rewritePassThroughResponsesModel(body, "  ")
	if err != nil {
		t.Fatalf("rewritePassThroughResponsesModel() error = %v", err)
	}
	if !bytes.Equal(rewritten, body) {
		t.Fatalf("body changed without an upstream model: got %s", rewritten)
	}
}

func TestRewritePassThroughResponsesModelRejectsInvalidJSON(t *testing.T) {
	if _, err := rewritePassThroughResponsesModel([]byte(`{"model":`), "provider-model"); err == nil {
		t.Fatal("rewritePassThroughResponsesModel() error = nil, want invalid JSON error")
	}
}
