package graphstream

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestIndependentReclaimEveryRename(t *testing.T) {
	for renamed := 0; renamed <= 2; renamed++ {
		t.Run(fmt.Sprint(renamed), func(t *testing.T) {
			dir, err := os.MkdirTemp("/tmp", "enola-review-crash-")
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("crash image %s", dir)
			j, err := OpenJournal(dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{"a", "b"} {
				if err = j.Append(JournalEntry{MsgID: id, Subject: "s", Payload: []byte("durable-" + id)}); err != nil {
					t.Fatal(err)
				}
			}
			if err = j.Ack("a"); err != nil {
				t.Fatal(err)
			}
			j.mu.Lock()
			keep := []string{"b"}
			next := map[string]*JournalEntry{"b": j.byID["b"]}
			for _, fn := range []func() error{
				func() error { return j.recordTombstonesLocked([]*JournalEntry{j.byID["a"]}, false) },
				j.flushDataLocked, j.closeFilesLocked,
				func() error { return j.rewrite("payloads.jsonl", keep, next, false) },
				func() error { return j.rewriteAcks(keep, next, false) },
				func() error { return j.writeCommitNamedLocked("commit.next.json") },
			} {
				if err := fn(); err != nil {
					j.mu.Unlock()
					t.Fatal(err)
				}
			}
			for _, name := range []string{"payloads.jsonl", "acks.jsonl"}[:renamed] {
				if err := os.Rename(filepath.Join(dir, name+".tmp"), filepath.Join(dir, name)); err != nil {
					j.mu.Unlock()
					t.Fatal(err)
				}
			}
			if err := syncDir(dir); err != nil {
				j.mu.Unlock()
				t.Fatal(err)
			}
			j.mu.Unlock()
			r, err := OpenJournal(dir)
			if err != nil {
				t.Fatalf("valid crash after %d compaction renames cannot recover durable b: %v", renamed, err)
			}
			defer r.Close()
			u := r.Unacked()
			if len(u) != 1 || u[0].MsgID != "b" || !bytes.Equal(u[0].Payload, []byte("durable-b")) {
				t.Fatalf("lost/changed durable b: %+v", u)
			}
		})
	}
}
func TestIndependentCompactEveryRename(t *testing.T) {
	for renamed := 0; renamed <= 3; renamed++ {
		t.Run(fmt.Sprint(renamed), func(t *testing.T) {
			dir, err := os.MkdirTemp("/tmp", "enola-review-compact-")
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("crash image %s", dir)
			j, err := OpenJournal(dir)
			if err != nil {
				t.Fatal(err)
			}
			a := MessageID("old-run", TypeBatch, 1)
			end := MessageID("old-run", TypeEndReplace, 2)
			for _, entry := range []JournalEntry{{MsgID: a, Subject: "s", Payload: []byte("data")}, {MsgID: end, Subject: "s", Payload: []byte(`{"type":"end_replace","run_id":"old-run"}`)}, {MsgID: "b", Subject: "s", Payload: []byte("durable-b")}} {
				if err = j.Append(entry); err != nil {
					t.Fatal(err)
				}
			}
			for _, id := range []string{a, end} {
				if err = j.Ack(id); err != nil {
					t.Fatal(err)
				}
			}
			j.mu.Lock()
			keep := []string{"b"}
			next := map[string]*JournalEntry{"b": j.byID["b"]}
			for _, fn := range []func() error{
				func() error { return j.recordTombstonesLocked([]*JournalEntry{j.byID[a], j.byID[end]}, true) },
				j.flushDataLocked, j.closeFilesLocked,
				func() error { return j.rewrite("payloads.jsonl", keep, next, false) }, j.writeEmptyAcksTmp,
				func() error { return j.rewriteTombsLocked(false) },
				func() error { return j.writeCommitNamedLocked("commit.next.json") },
			} {
				if err := fn(); err != nil {
					j.mu.Unlock()
					t.Fatal(err)
				}
			}
			for _, name := range []string{"payloads.jsonl", "acks.jsonl", "tombstones.jsonl"}[:renamed] {
				if err := os.Rename(filepath.Join(dir, name+".tmp"), filepath.Join(dir, name)); err != nil {
					j.mu.Unlock()
					t.Fatal(err)
				}
			}
			if err := syncDir(dir); err != nil {
				j.mu.Unlock()
				t.Fatal(err)
			}
			j.mu.Unlock()
			before := dirFingerprint(t, dir)
			r, err := OpenJournal(dir)
			if err != nil {
				t.Fatalf("valid crash after %d compact renames cannot recover: %v", renamed, err)
			}
			if dirFingerprint(t, dir) != before {
				t.Fatal("Open mutated recovery image")
			}
			u := r.Unacked()
			if len(u) != 1 || u[0].MsgID != "b" || !bytes.Equal(u[0].Payload, []byte("durable-b")) {
				t.Fatalf("lost/changed durable b: %+v", u)
			}
			if err := r.Append(JournalEntry{MsgID: a, Subject: "s", Payload: []byte("conflict")}); err == nil {
				t.Fatal("retired conflict accepted")
			}
			if err := r.Append(JournalEntry{MsgID: "fresh", Subject: "s", Payload: []byte("fresh")}); err != nil {
				t.Fatalf("first mutation after crash: %v", err)
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			r, err = OpenJournal(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			if len(r.Unacked()) != 2 {
				t.Fatalf("post-recovery durable entries %v", r.Unacked())
			}
		})
	}
}
