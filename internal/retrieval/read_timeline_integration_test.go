//go:build integration

package retrieval

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestReadTimeline_Continuation(t *testing.T) {
	db, engine := readFixture(t)
	readExec(t, db, `INSERT INTO relation(id,source_document_id,target_document_id,source_key,target_key,source_number,target_number,kind,raw_code,raw_label,evidence,method,artifact_sha256,ordinal)
	 SELECT n,NULL,1,'vn:actor'||lpad(n::text,3,'0'),'vn:1','N2','N1','repeals','','','','text_rule/1',repeat('b',64),n FROM generate_series(1,120) n`)
	flag := true
	first := readItems(t, engine, ReadItem{Handle: "vn:1", Timeline: &flag}).Items[0]
	if first.Next == "" || first.Complete {
		t.Fatalf("first page = %+v", first)
	}
	units := append([]Unit(nil), first.Units...)
	for first.Next != "" {
		next := ReadItem{Next: first.Next}
		alone := readItems(t, engine, next).Items[0]
		batch := readItems(t, engine, ReadItem{Handle: "vn:2"}, next).Items[1]
		if !reflect.DeepEqual(alone, batch) {
			t.Fatalf("continuation changed in batch: %+v / %+v", alone, batch)
		}
		first = batch
		if first.Error != "" {
			t.Fatalf("continuation error = %s", first.Error)
		}
		units = append(units, first.Units...)
	}
	if !first.Complete || len(units) != 121 {
		t.Fatalf("last page = %+v, count = %d", first, len(units))
	}
	if units[0].Event.Kind != "effective_from" {
		t.Fatalf("unexpected first event: %+v", units[0])
	}
	for i, unit := range units[1:] {
		want := fmt.Sprintf("vn:actor%03d", i+1)
		if unit.Event.Actor == nil || unit.Event.Actor.Key != want {
			t.Fatalf("event %d = %+v, want %s", i+1, unit, want)
		}
	}
}

func TestReadTimeline_Events(t *testing.T) {
	db, engine := readFixture(t)
	readExec(t, db, `UPDATE document SET issue_date='2020-01-01',eff_from='2020-01-01',eff_to='2021-01-01' WHERE id=1`)
	readExec(t, db, `UPDATE document SET issue_date='2020-01-01', number='  N  2  ' WHERE id=2`)
	readExec(t, db, `INSERT INTO relation(id,source_document_id,target_document_id,target_provision_id,source_key,target_key,source_number,target_number,kind,raw_code,raw_label,target_node_id,evidence,method,artifact_sha256,ordinal) VALUES
		(1,2,1,NULL,'vn:2','vn:1','N2','N1','repeals','','',NULL,'','text_rule/1',$1,1),
		(2,2,1,5,'vn:2','vn:1','N2','N1','repeals','','','p5','','text_rule/1',$1,2),
		(3,2,1,NULL,'vn:2','vn:1','N2','N1','suspends','','','missing','','text_rule/1',$1,3),
		(4,2,1,5,'vn:2','vn:1','N2','N1','replaces','','','p5/missing','','text_rule/1',$1,4),
		(5,NULL,1,NULL,'vn:missing','vn:1','M','N1','consolidates','','',NULL,'','vbpl_references',$1,5),
		(6,2,1,NULL,'vn:2','vn:1','N2','N1','cites','','',NULL,'','text_rule/1',$1,6),
		(7,2,1,NULL,'vn:2','vn:1','N2','N1','amends_supplements','','',NULL,'','text_rule/1',$1,7),
		(8,2,1,NULL,'vn:2','vn:1','N2','N1','consolidates','','',NULL,'','text_rule/1',$1,8),
		(9,1,2,NULL,'vn:1','vn:2','N1','N2','repeals','','',NULL,'','text_rule/1',$1,9),
		(10,2,NULL,NULL,'vn:2','vn:1','N2','N1','repeals','','',NULL,'','text_rule/1',$1,10)`, strings.Repeat("b", 64))
	flag := true
	result := readItems(t, engine, ReadItem{Handle: "vn:1", Timeline: &flag}).Items[0]
	if result.Error != "" || !result.Complete || result.Document != nil || result.Doc != "vn:1" {
		t.Fatalf("timeline result = %+v", result)
	}
	raw, err := json.Marshal(result.Units)
	if err != nil {
		t.Fatal(err)
	}
	var units []struct {
		Kind  string `json:"kind"`
		Event struct {
			Kind  string `json:"kind"`
			Date  string `json:"date"`
			Actor *struct {
				Key       string `json:"key"`
				Number    string `json:"number"`
				InRelease bool   `json:"in_release"`
			} `json:"actor"`
			Target *struct {
				Scope    string `json:"scope"`
				Named    string `json:"named"`
				Anchor   string `json:"anchor"`
				Citation string `json:"citation"`
			} `json:"target"`
		} `json:"event"`
	}
	if err := json.Unmarshal(raw, &units); err != nil {
		t.Fatal(err)
	}
	want := []string{"issued", "effective_from", "repeals", "repeals", "replaces", "suspends", "amends_supplements", "consolidates", "effective_to", "consolidates"}
	if len(units) != len(want) {
		t.Fatalf("units = %s", raw)
	}
	for i, kind := range want {
		if units[i].Kind != "timeline" || units[i].Event.Kind != kind {
			t.Fatalf("units = %s", raw)
		}
	}
	if units[2].Event.Actor.Number != "N 2" {
		t.Fatalf("actor number was not normalized: %s", raw)
	}
	if units[2].Event.Target.Scope != "document" || units[3].Event.Target.Scope != "provision" || units[3].Event.Target.Anchor != "p5" || units[3].Event.Target.Citation == "" || units[4].Event.Target.Scope != "unresolved_provision" || units[4].Event.Target.Named != "p5/missing" || units[5].Event.Target.Named != "missing" || units[9].Event.Actor.Key != "vn:missing" || units[9].Event.Actor.InRelease {
		t.Fatalf("units = %s", raw)
	}
}
