package config

import (
	"testing"

	"github.com/feivpn/feivpn-runtime/internal/feiapi"
)

func TestSelectNode_PreferPrimaryThenBackup(t *testing.T) {
	p := &Profile{PreferredCountry: "HK"}
	nodes := []feiapi.SubscriptionNode{
		{Name: "香港备用 HK - 01", AccessKey: "ss://backup"},
		{Name: "香港 HK - 02", AccessKey: "ss://primary"},
	}
	got, err := p.SelectNode(nodes)
	if err != nil {
		t.Fatalf("SelectNode error: %v", err)
	}
	if got.AccessKey != "ss://primary" {
		t.Fatalf("expected primary first, got %q", got.AccessKey)
	}
}

func TestSelectNode_FallbackToBackup(t *testing.T) {
	p := &Profile{PreferredCountry: "HK"}
	nodes := []feiapi.SubscriptionNode{
		{Name: "香港备用 HK - 01", AccessKey: "ss://backup-1"},
		{Name: "香港备用 HK - 02", AccessKey: "ss://backup-2"},
	}
	got, err := p.SelectNode(nodes)
	if err != nil {
		t.Fatalf("SelectNode error: %v", err)
	}
	if got.AccessKey != "ss://backup-1" {
		t.Fatalf("expected first backup when no primary, got %q", got.AccessKey)
	}
}

func TestSelectNode_AcceptsThreeLetterCode(t *testing.T) {
	p := &Profile{PreferredCountry: "KOR"}
	nodes := []feiapi.SubscriptionNode{
		{Name: "韩国 KOR - 01", AccessKey: "ss://kor"},
	}
	got, err := p.SelectNode(nodes)
	if err != nil {
		t.Fatalf("SelectNode error: %v", err)
	}
	if got.AccessKey != "ss://kor" {
		t.Fatalf("wrong node: %q", got.AccessKey)
	}
}
