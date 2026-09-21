package graphstream

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc64"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	maxJournalLine         = 16 * 1024 * 1024
	defaultJournalMaxBytes = 64 << 20
	journalBufSize         = 256 * 1024
	commitSchema           = "enola.graphjournal.commit.v1"
)

var journalCRC = crc64.MakeTable(crc64.ECMA)

// JournalEntry is one published (or attempted) protocol message.
type JournalEntry struct {
	MsgID   string `json:"msg_id"`
	Subject string `json:"subject"`
	Payload []byte `json:"payload"`
	Acked   bool   `json:"acked"`
	isEnd   bool
	endRun  string
	lineN   int64
}

type payloadLine struct {
	MsgID   string `json:"msg_id"`
	Subject string `json:"subject"`
	Payload []byte `json:"payload"`
}

type ackLine struct {
	MsgID string `json:"msg_id"`
}

type tombstoneLine struct {
	MsgID   string `json:"msg_id"`
	Subject string `json:"subject"`
	SHA256  string `json:"sha256"`
}

type fenceLine struct {
	RunID string `json:"run_id"`
}

type commitMeta struct {
	Size  int64  `json:"size"`
	CRC64 uint64 `json:"crc64"`
}

type commitFile struct {
	Schema string                `json:"schema"`
	Files  map[string]commitMeta `json:"files"`
}

// Journal is append-only: payloads.jsonl, acks.jsonl, tombstones.jsonl, and
// optional fences.jsonl / commit.json.
//
// maxBytes bounds the retained directory after mutation: payloads, acks,
// exact identity tombstones (msg_id + subject + SHA-256), run fences, and
// commit.json. It is not a total-output cap. Live reclaim drops acked non-End
// payloads; their exact identities are retained until CompactAcked retires a
// protocol run or identity history hits the cap (recoverable failure). During
// reclaim a sibling .tmp may exist, so peak directory usage can approach 2×
// maxBytes.
//
// OpenJournal is read-only with respect to existing files: it does not reclaim,
// truncate, create, or fsync. Mutation (create, truncate of an uncommitted
// tail, reclaim, compact, commit index) happens on Append/Ack/CompactAcked/Sync.
type Journal struct {
	mu           sync.Mutex
	syncWait     *sync.Cond
	dir          string
	order        []string
	byID         map[string]*JournalEntry
	tomb         map[string]tombstoneLine
	fenced       map[string]struct{}
	maxBytes     int64
	physical     int64
	unackedBytes int64
	ackedWaste   int64
	tombBytes    int64
	fenceBytes   int64
	commitBytes  int64

	payloadFile *os.File
	ackFile     *os.File
	tombFile    *os.File
	fenceFile   *os.File
	payloadBuf  *bufio.Writer
	ackBuf      *bufio.Writer
	tombBuf     *bufio.Writer
	fenceBuf    *bufio.Writer
	unsynced    bool
	syncing     bool
	syncHook    func()
	syncs       int64
	dirReady    bool
	lineScratch []byte

	payloadValid  int64
	ackValid      int64
	tombValid     int64
	fenceValid    int64
	payloadCRC    uint64
	ackCRC        uint64
	tombCRC       uint64
	fenceCRC      uint64
	payloadTorn   bool
	ackTorn       bool
	tombTorn      bool
	fenceTorn     bool
	payloadNeedNL bool
	ackNeedNL     bool
	tombNeedNL    bool
	fenceNeedNL   bool

	hasCommit      bool
	commit         map[string]commitMeta
	readPath       map[string]string
	pendingInstall bool
}

// OpenJournal loads an existing log or starts empty. dir is the journal directory.
// Loading does not mutate the directory.
func OpenJournal(path string) (*Journal, error) {
	return openJournal(path, defaultJournalMaxBytes)
}

func openJournal(path string, maxBytes int64) (*Journal, error) {
	dir := path
	if filepath.Ext(path) == ".jsonl" {
		dir = filepath.Dir(path)
	}
	if maxBytes <= 0 {
		maxBytes = defaultJournalMaxBytes
	}
	j := &Journal{
		dir:      dir,
		byID:     map[string]*JournalEntry{},
		tomb:     map[string]tombstoneLine{},
		fenced:   map[string]struct{}{},
		maxBytes: maxBytes,
	}
	j.syncWait = sync.NewCond(&j.mu)
	if err := j.loadCommit(); err != nil {
		return nil, err
	}
	if err := j.loadPayloads(); err != nil {
		return nil, err
	}
	if err := j.loadAcks(); err != nil {
		return nil, err
	}
	if err := j.loadTombstones(); err != nil {
		return nil, err
	}
	if err := j.loadFences(); err != nil {
		return nil, err
	}
	return j, nil
}

func (j *Journal) loadCommit() error {
	primary, primaryBytes, err := readCommitFile(filepath.Join(j.dir, "commit.json"))
	if err != nil {
		return err
	}
	next, _, nextErr := readCommitFile(filepath.Join(j.dir, "commit.next.json"))
	if nextErr != nil {
		return nextErr
	}
	if next != nil {
		if paths, ok := j.resolveManifest(next.Files); ok {
			j.hasCommit = true
			j.commit = next.Files
			j.commitBytes = int64(len(mustMarshalCommit(next)))
			j.readPath = paths
			j.pendingInstall = true
			return nil
		}
	}
	if primary == nil {
		return nil
	}
	j.hasCommit = true
	j.commit = primary.Files
	if j.commit == nil {
		j.commit = map[string]commitMeta{}
	}
	j.commitBytes = primaryBytes
	return nil
}

func readCommitFile(path string) (*commitFile, int64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	if len(b) == 0 {
		return nil, 0, nil
	}
	var c commitFile
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, 0, fmt.Errorf("graphstream journal %s: committed corruption: %w", filepath.Base(path), err)
	}
	if c.Schema != "" && c.Schema != commitSchema {
		return nil, 0, fmt.Errorf("graphstream journal %s: unknown schema %q", filepath.Base(path), c.Schema)
	}
	if c.Files == nil {
		c.Files = map[string]commitMeta{}
	}
	return &c, int64(len(b)), nil
}

func mustMarshalCommit(c *commitFile) []byte {
	b, _ := json.Marshal(c)
	return b
}

func (j *Journal) fileMatchesMeta(path string, m commitMeta) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return m.Size == 0 && os.IsNotExist(err)
	}
	return int64(len(b)) == m.Size && crc64.Checksum(b, journalCRC) == m.CRC64
}

func (j *Journal) resolveManifest(files map[string]commitMeta) (map[string]string, bool) {
	if files == nil {
		return nil, false
	}
	paths := map[string]string{}
	for name, m := range files {
		live := filepath.Join(j.dir, name)
		tmp := live + ".tmp"
		switch {
		case j.fileMatchesMeta(live, m):
			paths[name] = live
		case j.fileMatchesMeta(tmp, m):
			paths[name] = tmp
		default:
			return nil, false
		}
	}
	return paths, true
}

func (j *Journal) logPath(name string) string {
	if j.readPath != nil {
		if p, ok := j.readPath[name]; ok {
			return p
		}
	}
	return filepath.Join(j.dir, name)
}

func (j *Journal) committed(name string) (commitMeta, bool) {
	if !j.hasCommit {
		return commitMeta{}, false
	}
	m, ok := j.commit[name]
	return m, ok
}

func (j *Journal) loadPayloads() error {
	lines, valid, torn, needNL, crc, err := j.readLog("payloads.jsonl")
	if err != nil {
		return err
	}
	j.payloadValid = valid
	j.payloadTorn = torn
	j.payloadNeedNL = needNL
	j.payloadCRC = crc
	for _, b := range lines {
		var p payloadLine
		if err := json.Unmarshal(b, &p); err != nil {
			return fmt.Errorf("graphstream journal payloads.jsonl: committed corruption: %w", err)
		}
		cp := bytes.Clone(p.Payload)
		e := &JournalEntry{MsgID: p.MsgID, Subject: p.Subject, Payload: cp, lineN: int64(len(b) + 1)}
		annotateEnd(e)
		j.byID[p.MsgID] = e
		j.order = append(j.order, p.MsgID)
		j.unackedBytes += e.lineN
		j.physical += e.lineN
	}
	return nil
}

func (j *Journal) loadAcks() error {
	lines, valid, torn, needNL, crc, err := j.readLog("acks.jsonl")
	if err != nil {
		return err
	}
	j.ackValid = valid
	j.ackTorn = torn
	j.ackNeedNL = needNL
	j.ackCRC = crc
	for _, b := range lines {
		var a ackLine
		if err := json.Unmarshal(b, &a); err != nil {
			return fmt.Errorf("graphstream journal acks.jsonl: committed corruption: %w", err)
		}
		j.physical += int64(len(b) + 1)
		if e := j.byID[a.MsgID]; e != nil && !e.Acked {
			e.Acked = true
			n := e.encodedSize()
			j.unackedBytes -= n
			if j.unackedBytes < 0 {
				j.unackedBytes = 0
			}
			if !e.isEnd {
				j.ackedWaste += n
			}
		}
	}
	return nil
}

func (j *Journal) loadTombstones() error {
	lines, valid, torn, needNL, crc, err := j.readLog("tombstones.jsonl")
	if err != nil {
		return err
	}
	j.tombValid = valid
	j.tombTorn = torn
	j.tombNeedNL = needNL
	j.tombCRC = crc
	j.tombBytes = valid
	for _, b := range lines {
		var t tombstoneLine
		if err := json.Unmarshal(b, &t); err != nil {
			return fmt.Errorf("graphstream journal tombstones.jsonl: committed corruption: %w", err)
		}
		if t.MsgID != "" {
			j.tomb[t.MsgID] = t
		}
	}
	return nil
}

func (j *Journal) loadFences() error {
	lines, valid, torn, needNL, crc, err := j.readLog("fences.jsonl")
	if err != nil {
		return err
	}
	j.fenceValid = valid
	j.fenceTorn = torn
	j.fenceNeedNL = needNL
	j.fenceCRC = crc
	j.fenceBytes = valid
	for _, b := range lines {
		var f fenceLine
		if err := json.Unmarshal(b, &f); err != nil {
			return fmt.Errorf("graphstream journal fences.jsonl: committed corruption: %w", err)
		}
		if f.RunID != "" {
			j.fenced[f.RunID] = struct{}{}
		}
	}
	return nil
}

func (j *Journal) readLog(name string) (lines [][]byte, validEnd int64, torn, needNL bool, crc uint64, err error) {
	path := j.logPath(name)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if m, ok := j.committed(name); ok && m.Size > 0 {
				return nil, 0, false, false, 0, fmt.Errorf("graphstream journal %s: committed tail missing: have 0 want %d", name, m.Size)
			}
			return nil, 0, false, false, 0, nil
		}
		return nil, 0, false, false, 0, err
	}
	if m, ok := j.committed(name); ok {
		if int64(len(raw)) < m.Size {
			return nil, 0, false, false, 0, fmt.Errorf("graphstream journal %s: committed tail missing: have %d want %d", name, len(raw), m.Size)
		}
		prefix := raw[:m.Size]
		got := crc64.Checksum(prefix, journalCRC)
		if got != m.CRC64 {
			return nil, 0, false, false, 0, fmt.Errorf("graphstream journal %s: committed checksum mismatch", name)
		}
		lines, validEnd, torn, needNL, err = splitJournalLines(name, prefix, true)
		if err != nil {
			return nil, 0, false, false, 0, err
		}
		if int64(len(raw)) > m.Size {
			torn = true
		}
		return lines, m.Size, torn, needNL, m.CRC64, nil
	}
	lines, validEnd, torn, needNL, err = splitJournalLines(name, raw, false)
	if err != nil {
		return nil, 0, false, false, 0, err
	}
	if validEnd > 0 && int64(len(raw)) >= validEnd {
		crc = crc64.Checksum(raw[:validEnd], journalCRC)
	}
	return lines, validEnd, torn, needNL, crc, nil
}

// splitJournalLines returns complete committed JSON lines.
//
// A last record without a newline is retained when it is valid JSON (content
// integrity of the object is provable). Newline framing is not treated as the
// commit evidence. An invalid last record without a newline is an uncommitted
// torn tail and is skipped. A malformed record that is followed by a newline
// is committed corruption, including when it is the last record in the file
// (bytes.Split leaves an empty trailing part).
func splitJournalLines(name string, raw []byte, committedPrefix bool) (lines [][]byte, validEnd int64, torn, needNL bool, err error) {
	if len(raw) == 0 {
		return nil, 0, false, false, nil
	}
	endedWithNL := raw[len(raw)-1] == '\n'
	parts := bytes.Split(raw, []byte{'\n'})
	var offset int64
	for i, part := range parts {
		if len(part) == 0 {
			if i < len(parts)-1 {
				offset++
			}
			continue
		}
		isLast := i == len(parts)-1
		if isLast && !endedWithNL {
			if len(part) > maxJournalLine {
				if committedPrefix {
					return nil, 0, false, false, fmt.Errorf("graphstream journal %s: committed corruption: line %d exceeds %d", name, len(part), maxJournalLine)
				}
				return lines, offset, true, false, nil
			}
			if json.Valid(part) {
				lines = append(lines, bytes.Clone(part))
				offset += int64(len(part))
				return lines, offset, true, true, nil
			}
			if committedPrefix {
				return nil, 0, false, false, fmt.Errorf("graphstream journal %s: committed corruption at offset %d", name, offset)
			}
			return lines, offset, true, false, nil
		}
		if len(part) > maxJournalLine {
			if isLast && !committedPrefix {
				return lines, offset, true, false, nil
			}
			return nil, 0, false, false, fmt.Errorf("graphstream journal %s: committed corruption: line %d exceeds %d", name, len(part), maxJournalLine)
		}
		if !json.Valid(part) {
			if isLast && !endedWithNL && !committedPrefix {
				return lines, offset, true, false, nil
			}
			return nil, 0, false, false, fmt.Errorf("graphstream journal %s: committed corruption at offset %d", name, offset)
		}
		lines = append(lines, bytes.Clone(part))
		offset += int64(len(part) + 1)
	}
	return lines, offset, false, false, nil
}

func annotateEnd(e *JournalEntry) {
	if e == nil {
		return
	}
	meta := inspectPayload(e.Payload)
	e.endRun, e.isEnd = meta.endRun, meta.isEnd
}

func (e *JournalEntry) encodedSize() int64 {
	if e == nil {
		return 0
	}
	if e.lineN > 0 {
		return e.lineN
	}
	return encodedPayloadSize(e)
}

func encodedPayloadSize(e *JournalEntry) int64 {
	if e == nil {
		return 0
	}
	b, err := json.Marshal(payloadLine{MsgID: e.MsgID, Subject: e.Subject, Payload: e.Payload})
	if err != nil {
		return int64(len(e.Payload))
	}
	return int64(len(b) + 1)
}

func payloadHash(subject string, payload []byte) tombstoneLine {
	sum := sha256.Sum256(payload)
	return tombstoneLine{Subject: subject, SHA256: hex.EncodeToString(sum[:])}
}

func parseProtocolRun(msgID string) (string, bool) {
	i := strings.LastIndexByte(msgID, ':')
	if i <= 0 {
		return "", false
	}
	seq := msgID[i+1:]
	if seq == "" || !allDigits(seq) {
		return "", false
	}
	rest := msgID[:i]
	j := strings.LastIndexByte(rest, ':')
	if j <= 0 {
		return "", false
	}
	typ := rest[j+1:]
	switch typ {
	case TypeBeginReplace, TypeBatch, TypeEndReplace:
		run := rest[:j]
		if run == "" {
			return "", false
		}
		return run, true
	default:
		return "", false
	}
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func (j *Journal) ensureDirLocked() error {
	if j.dirReady {
		return nil
	}
	if err := os.MkdirAll(j.dir, 0o755); err != nil {
		return err
	}
	j.dirReady = true
	return nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func (j *Journal) installPendingLocked() error {
	if !j.pendingInstall {
		return nil
	}
	for name, src := range j.readPath {
		dest := filepath.Join(j.dir, name)
		if src == dest {
			continue
		}
		if err := os.Rename(src, dest); err != nil {
			return err
		}
	}
	if err := syncDir(j.dir); err != nil {
		return err
	}
	if err := j.writeCommitLocked(); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(j.dir, "commit.next.json"))
	j.pendingInstall = false
	j.readPath = nil
	return syncDir(j.dir)
}

func (j *Journal) filesOpenLocked() bool {
	return j.payloadFile != nil && j.ackFile != nil
}

func (j *Journal) openFilesLocked() error {
	if j.filesOpenLocked() {
		return nil
	}
	if err := j.ensureDirLocked(); err != nil {
		return err
	}
	if err := j.installPendingLocked(); err != nil {
		return err
	}
	if err := j.openOneLocked("payloads.jsonl", &j.payloadFile, &j.payloadBuf, j.payloadValid, j.payloadTorn, j.payloadNeedNL, &j.payloadValid, &j.payloadTorn, &j.payloadNeedNL, &j.payloadCRC); err != nil {
		return err
	}
	return j.openOneLocked("acks.jsonl", &j.ackFile, &j.ackBuf, j.ackValid, j.ackTorn, j.ackNeedNL, &j.ackValid, &j.ackTorn, &j.ackNeedNL, &j.ackCRC)
}

func (j *Journal) openTombLocked() error {
	if j.tombFile != nil {
		return nil
	}
	if err := j.ensureDirLocked(); err != nil {
		return err
	}
	return j.openOneLocked("tombstones.jsonl", &j.tombFile, &j.tombBuf, j.tombValid, j.tombTorn, j.tombNeedNL, &j.tombValid, &j.tombTorn, &j.tombNeedNL, &j.tombCRC)
}

func (j *Journal) openFenceLocked() error {
	if j.fenceFile != nil {
		return nil
	}
	if err := j.ensureDirLocked(); err != nil {
		return err
	}
	return j.openOneLocked("fences.jsonl", &j.fenceFile, &j.fenceBuf, j.fenceValid, j.fenceTorn, j.fenceNeedNL, &j.fenceValid, &j.fenceTorn, &j.fenceNeedNL, &j.fenceCRC)
}

func (j *Journal) openOneLocked(name string, f **os.File, w **bufio.Writer, valid int64, torn, needNL bool, validOut *int64, tornOut *bool, needNLOut *bool, crc *uint64) error {
	if *f != nil {
		return nil
	}
	path := filepath.Join(j.dir, name)
	_, statErr := os.Stat(path)
	created := os.IsNotExist(statErr)
	fh, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	fi, err := fh.Stat()
	if err != nil {
		fh.Close()
		return err
	}
	if torn && fi.Size() > valid {
		if err := fh.Truncate(valid); err != nil {
			fh.Close()
			return err
		}
		if err := fh.Sync(); err != nil {
			fh.Close()
			return err
		}
		*tornOut = false
		*validOut = valid
	} else if !torn {
		*validOut = fi.Size()
	}
	if _, err := fh.Seek(0, io.SeekEnd); err != nil {
		fh.Close()
		return err
	}
	if needNL {
		if _, err := fh.Write([]byte{'\n'}); err != nil {
			fh.Close()
			return err
		}
		*crc = crc64.Update(*crc, journalCRC, []byte{'\n'})
		*validOut += 1
		*needNLOut = false
		j.unsynced = true
		if name == "payloads.jsonl" || name == "acks.jsonl" {
			j.physical++
		}
		if name == "tombstones.jsonl" {
			j.tombBytes++
		}
	}
	*f = fh
	*w = bufio.NewWriterSize(fh, journalBufSize)
	if created {
		if err := fh.Sync(); err != nil {
			return err
		}
		if err := syncDir(j.dir); err != nil {
			return err
		}
		j.syncs++
	}
	return nil
}

func (j *Journal) closeFilesLocked() error {
	var first error
	closeOne := func(w **bufio.Writer, f **os.File) {
		if *w != nil {
			if err := (*w).Flush(); err != nil && first == nil {
				first = err
			}
			*w = nil
		}
		if *f != nil {
			if err := (*f).Close(); err != nil && first == nil {
				first = err
			}
			*f = nil
		}
	}
	closeOne(&j.payloadBuf, &j.payloadFile)
	closeOne(&j.ackBuf, &j.ackFile)
	closeOne(&j.tombBuf, &j.tombFile)
	closeOne(&j.fenceBuf, &j.fenceFile)
	return first
}

func (j *Journal) writeRawLocked(kind string, b []byte) error {
	if err := j.openFilesLocked(); err != nil {
		return err
	}
	if len(b)+1 > maxJournalLine {
		return fmt.Errorf("graphstream journal: line %d exceeds %d", len(b), maxJournalLine)
	}
	var w *bufio.Writer
	var valid *int64
	var crc *uint64
	switch kind {
	case "payloads.jsonl":
		w = j.payloadBuf
		valid = &j.payloadValid
		crc = &j.payloadCRC
	case "acks.jsonl":
		w = j.ackBuf
		valid = &j.ackValid
		crc = &j.ackCRC
	case "tombstones.jsonl":
		if err := j.openTombLocked(); err != nil {
			return err
		}
		w = j.tombBuf
		valid = &j.tombValid
		crc = &j.tombCRC
	case "fences.jsonl":
		if err := j.openFenceLocked(); err != nil {
			return err
		}
		w = j.fenceBuf
		valid = &j.fenceValid
		crc = &j.fenceCRC
	default:
		return fmt.Errorf("graphstream journal: unknown log %s", kind)
	}
	j.lineScratch = append(j.lineScratch[:0], b...)
	j.lineScratch = append(j.lineScratch, '\n')
	n, err := w.Write(j.lineScratch)
	if err != nil {
		return err
	}
	*crc = crc64.Update(*crc, journalCRC, j.lineScratch)
	*valid += int64(n)
	switch kind {
	case "payloads.jsonl", "acks.jsonl":
		j.physical += int64(n)
	case "tombstones.jsonl":
		j.tombBytes += int64(n)
	case "fences.jsonl":
		j.fenceBytes += int64(n)
	}
	j.unsynced = true
	return nil
}

func (j *Journal) waitNotSyncingLocked() {
	for j.syncing {
		j.syncWait.Wait()
	}
}

// Sync flushes buffered payload, ack, identity, and fence writes, fsyncs those
// files, then durably records a commit index of their committed prefixes.
func (j *Journal) Sync() error {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.ackedWaste > 0 && j.dirBytesLocked() > j.boundLocked() {
		if err := j.reclaimLiveLocked(); err != nil {
			return err
		}
	}
	return j.syncLocked()
}

// Close flushes, fsyncs, and closes journal file handles. It is safe on nil
// and on a journal that never opened files (read-only OpenJournal). Further
// Append/Ack/CompactAcked reopen handles. Double-close is safe.
func (j *Journal) Close() error {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	err := j.syncLocked()
	err2 := j.closeFilesLocked()
	if err != nil {
		return err
	}
	return err2
}

func (j *Journal) flushDataLocked() error {
	for _, w := range []*bufio.Writer{j.payloadBuf, j.ackBuf, j.tombBuf, j.fenceBuf} {
		if w != nil {
			if err := w.Flush(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (j *Journal) fsyncFiles(files []*os.File) error {
	for _, f := range files {
		if f != nil {
			if err := f.Sync(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (j *Journal) syncLocked() error {
	if !j.unsynced && j.hasCommit {
		return nil
	}
	if !j.unsynced && !j.hasCommit {
		if j.payloadFile == nil && j.ackFile == nil && j.tombFile == nil && j.fenceFile == nil {
			return nil
		}
	}
	if err := j.flushDataLocked(); err != nil {
		return err
	}
	files := []*os.File{j.payloadFile, j.ackFile, j.tombFile, j.fenceFile}
	hook := j.syncHook
	j.unsynced = false
	j.syncing = true
	j.mu.Unlock()
	if hook != nil {
		hook()
	}
	err := j.fsyncFiles(files)
	j.mu.Lock()
	j.syncing = false
	j.syncWait.Broadcast()
	if err != nil {
		j.unsynced = true
		return err
	}
	if err := j.writeCommitLocked(); err != nil {
		j.unsynced = true
		return err
	}
	j.syncs++
	return nil
}

func (j *Journal) writeCommitLocked() error {
	return j.writeCommitNamedLocked("commit.json")
}

func (j *Journal) writeCommitNamedLocked(name string) error {
	if err := j.ensureDirLocked(); err != nil {
		return err
	}
	c := commitFile{
		Schema: commitSchema,
		Files: map[string]commitMeta{
			"payloads.jsonl":   {Size: j.payloadValid, CRC64: j.payloadCRC},
			"acks.jsonl":       {Size: j.ackValid, CRC64: j.ackCRC},
			"tombstones.jsonl": {Size: j.tombValid, CRC64: j.tombCRC},
			"fences.jsonl":     {Size: j.fenceValid, CRC64: j.fenceCRC},
		},
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	path := filepath.Join(j.dir, name)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	tf, err := os.Open(tmp)
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if err := tf.Sync(); err != nil {
		tf.Close()
		os.Remove(tmp)
		return err
	}
	if err := tf.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := syncDir(j.dir); err != nil {
		return err
	}
	j.hasCommit = true
	j.commit = c.Files
	j.commitBytes = int64(len(b))
	return nil
}

// SetMaxBytes sets the retained physical spool bound. Zero or negative restores the default.
func (j *Journal) SetMaxBytes(n int64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if n <= 0 {
		n = defaultJournalMaxBytes
	}
	j.maxBytes = n
}

// RetainedBytes is the retained physical size of payloads.jsonl plus acks.jsonl.
func (j *Journal) RetainedBytes() int64 {
	return j.PhysicalBytes()
}

// PhysicalBytes is the retained physical size of payloads.jsonl plus acks.jsonl
// (JSON encoding, including base64 payload expansion and ack records).
func (j *Journal) PhysicalBytes() int64 {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.physical
}

func (j *Journal) dirBytesLocked() int64 {
	n := j.physical + j.tombBytes + j.fenceBytes + j.commitBytes
	if n < 0 {
		return 0
	}
	return n
}

// SetSyncHook runs during Sync after buffers are flushed and the map lock is
// released, before fsync. Tests use it to stall durability without blocking
// identity lookups or async enqueue.
func (j *Journal) SetSyncHook(fn func()) {
	if j == nil {
		return
	}
	j.mu.Lock()
	j.syncHook = fn
	j.mu.Unlock()
}

func (j *Journal) boundLocked() int64 {
	if j.maxBytes <= 0 {
		return defaultJournalMaxBytes
	}
	return j.maxBytes
}

func (j *Journal) peakBoundLocked() int64 {
	return 2 * j.boundLocked()
}

// SyncCount is the number of completed group fsyncs of journal files.
func (j *Journal) SyncCount() int64 {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.syncs
}

// HasAckedEnd reports whether an acknowledged EndReplace for runID is retained.
func (j *Journal) HasAckedEnd(runID string) bool {
	if j == nil || runID == "" {
		return false
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.hasAckedEndLocked(runID)
}

func (j *Journal) hasAckedEndLocked(runID string) bool {
	for _, id := range j.order {
		e := j.byID[id]
		if e == nil || !e.Acked || !e.isEnd {
			continue
		}
		if e.endRun == runID {
			return true
		}
	}
	return false
}

func isEndPayload(p []byte) bool {
	return endRunID(p) != "" || endType(p)
}

func endType(p []byte) bool {
	return envelopeType(p) == TypeEndReplace
}

func envelopeType(p []byte) string {
	var probe struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(p, &probe) != nil {
		return ""
	}
	return probe.Type
}

func endRunID(p []byte) string {
	var probe struct {
		Type  string `json:"type"`
		RunID string `json:"run_id"`
	}
	if json.Unmarshal(p, &probe) != nil || probe.Type != TypeEndReplace {
		return ""
	}
	return probe.RunID
}

func keepLive(e *JournalEntry) bool {
	if e == nil {
		return false
	}
	if !e.Acked {
		return true
	}
	return e.isEnd
}

func (j *Journal) completedLocked(msgID, subject string, payload []byte) error {
	if existing, ok := j.byID[msgID]; ok {
		if existing.Subject != subject || !bytes.Equal(existing.Payload, payload) {
			return fmt.Errorf("graphstream journal: msg id %s reused with different payload", msgID)
		}
		return nil
	}
	if ts, ok := j.tomb[msgID]; ok {
		want := payloadHash(subject, payload)
		if ts.Subject != subject || ts.SHA256 != want.SHA256 {
			return fmt.Errorf("graphstream journal: msg id %s reused with different payload", msgID)
		}
		return nil
	}
	if run, ok := parseProtocolRun(msgID); ok {
		if _, fenced := j.fenced[run]; fenced {
			return fmt.Errorf("graphstream journal: msg id %s belongs to retired run %s", msgID, run)
		}
	}
	return errNotFound
}

func (j *Journal) checkIdentity(msgID, subject string, payload []byte) error {
	if j == nil {
		return errNotFound
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.completedLocked(msgID, subject, payload)
}

// asyncAdmit is used by the commit worker. skip means the identity is already
// durably complete with the same payload and must not go to the sink.
func (j *Journal) asyncAdmit(msgID, subject string, payload []byte) (skip bool, err error) {
	if j == nil {
		return false, nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	switch err := j.completedLocked(msgID, subject, payload); err {
	case errNotFound:
		return false, nil
	case nil:
		if e, ok := j.byID[msgID]; ok && !e.Acked {
			return false, nil
		}
		return true, nil
	default:
		return false, err
	}
}

var errNotFound = fmt.Errorf("graphstream journal: not found")

// Append records a payload before the sink publish. A second call with the same
// MsgID must carry identical subject and payload; it does not create a new row.
// Append is durable: it group-commits (fsyncs) before returning.
func (j *Journal) Append(e JournalEntry) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := j.appendLocked(e, nil); err != nil {
		return err
	}
	return j.syncLocked()
}

func (j *Journal) appendUnsynced(e JournalEntry) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.appendLocked(e, nil)
}

func (j *Journal) appendClassifiedUnsynced(e JournalEntry, meta *envelopeMetadata) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.appendLocked(e, meta)
}

func (j *Journal) appendLocked(e JournalEntry, meta *envelopeMetadata) error {
	j.waitNotSyncingLocked()
	cp := bytes.Clone(e.Payload)
	switch err := j.completedLocked(e.MsgID, e.Subject, cp); err {
	case nil:
		return nil
	case errNotFound:
	default:
		return err
	}
	line, err := json.Marshal(payloadLine{MsgID: e.MsgID, Subject: e.Subject, Payload: cp})
	if err != nil {
		return err
	}
	need := int64(len(line) + 1)
	if j.maxBytes > 0 && need > j.maxBytes {
		return fmt.Errorf("graphstream journal: payload %d exceeds spool %d bytes", need, j.maxBytes)
	}
	if j.maxBytes > 0 && (j.physical+need > j.maxBytes || j.dirBytesLocked()+need > j.maxBytes) {
		if err := j.reclaimLiveLocked(); err != nil {
			return err
		}
		if j.physical+need > j.maxBytes || j.dirBytesLocked()+need > j.maxBytes {
			return fmt.Errorf("graphstream journal: spool exceeds %d bytes", j.maxBytes)
		}
	}
	if err := j.writeRawLocked("payloads.jsonl", line); err != nil {
		return err
	}
	ent := &JournalEntry{MsgID: e.MsgID, Subject: e.Subject, Payload: cp, lineN: need}
	if meta == nil {
		annotateEnd(ent)
	} else {
		ent.endRun, ent.isEnd = meta.endRun, meta.isEnd
	}
	j.byID[e.MsgID] = ent
	j.order = append(j.order, e.MsgID)
	j.unackedBytes += need
	return nil
}

func (j *Journal) alreadyComplete(msgID string) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if e, ok := j.byID[msgID]; ok && e.Acked {
		return true
	}
	_, ok := j.tomb[msgID]
	return ok
}

// Ack marks MsgID acknowledged with an append-only ack record.
// Ack is durable: it group-commits before returning.
func (j *Journal) Ack(msgID string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := j.ackLocked(msgID); err != nil {
		return err
	}
	return j.syncLocked()
}

func (j *Journal) ackUnsynced(msgID string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.ackLocked(msgID)
}

func (j *Journal) ackLocked(msgID string) error {
	j.waitNotSyncingLocked()
	if _, ok := j.tomb[msgID]; ok {
		return nil
	}
	e, ok := j.byID[msgID]
	if !ok {
		return fmt.Errorf("graphstream journal: unknown msg id %s", msgID)
	}
	if e.Acked {
		return nil
	}
	line, err := json.Marshal(ackLine{MsgID: msgID})
	if err != nil {
		return err
	}
	need := int64(len(line) + 1)
	if j.maxBytes > 0 && j.physical+need > j.maxBytes {
		if err := j.reclaimLiveLocked(); err != nil {
			return err
		}
	}
	if err := j.writeRawLocked("acks.jsonl", line); err != nil {
		return err
	}
	e.Acked = true
	n := e.encodedSize()
	j.unackedBytes -= n
	if j.unackedBytes < 0 {
		j.unackedBytes = 0
	}
	if !e.isEnd {
		j.ackedWaste += n
		if j.maxBytes > 0 && (j.ackedWaste >= j.maxBytes/2 || j.physical > j.maxBytes) {
			if err := j.reclaimLiveLocked(); err != nil {
				return err
			}
		}
	}
	return nil
}

// Unacked returns copies of entries that were written but not acknowledged.
func (j *Journal) Unacked() []JournalEntry {
	j.mu.Lock()
	defer j.mu.Unlock()
	var out []JournalEntry
	for _, id := range j.order {
		e := j.byID[id]
		if e != nil && !e.Acked {
			out = append(out, JournalEntry{MsgID: e.MsgID, Subject: e.Subject, Payload: bytes.Clone(e.Payload)})
		}
	}
	return out
}

// Get returns a copy of the stored entry for msgID.
func (j *Journal) Get(msgID string) (JournalEntry, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	e, ok := j.byID[msgID]
	if !ok {
		return JournalEntry{}, false
	}
	return JournalEntry{MsgID: e.MsgID, Subject: e.Subject, Payload: bytes.Clone(e.Payload), Acked: e.Acked}, true
}

// Entries returns copies of the log in append order.
func (j *Journal) Entries() []JournalEntry {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]JournalEntry, 0, len(j.order))
	for _, id := range j.order {
		e := j.byID[id]
		if e == nil {
			continue
		}
		out = append(out, JournalEntry{MsgID: e.MsgID, Subject: e.Subject, Payload: bytes.Clone(e.Payload), Acked: e.Acked})
	}
	return out
}

// CompactAcked drops acknowledged payloads after a committed generation,
// including EndReplace proofs (state.json is the checkpoint). Protocol-run
// identities covered by an acked EndReplace are retired behind a durable
// generation fence; later Append of those run IDs is rejected. Remaining
// arbitrary IDs keep exact msg_id/subject/SHA-256 tombstones.
func (j *Journal) CompactAcked() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := j.syncLocked(); err != nil {
		return err
	}
	var keep []string
	next := map[string]*JournalEntry{}
	var drop []*JournalEntry
	for _, id := range j.order {
		e := j.byID[id]
		if e == nil {
			continue
		}
		if e.Acked {
			drop = append(drop, e)
			continue
		}
		keep = append(keep, id)
		next[id] = e
	}
	if err := j.recordTombstonesLocked(drop, true); err != nil {
		return err
	}
	if err := j.flushDataLocked(); err != nil {
		return err
	}
	if err := j.closeFilesLocked(); err != nil {
		return err
	}
	if err := j.rewrite("payloads.jsonl", keep, next, false); err != nil {
		_ = j.openFilesLocked()
		return err
	}
	if err := j.writeEmptyAcksTmp(); err != nil {
		_ = j.openFilesLocked()
		return err
	}
	if err := j.rewriteTombsLocked(false); err != nil {
		_ = j.openFilesLocked()
		return err
	}
	if err := j.installRewritesLocked("payloads.jsonl", "acks.jsonl", "tombstones.jsonl"); err != nil {
		_ = j.openFilesLocked()
		return err
	}
	var size int64
	for _, id := range keep {
		size += next[id].encodedSize()
	}
	j.order = keep
	j.byID = next
	j.physical = size
	j.unackedBytes = size
	j.ackedWaste = 0
	j.unsynced = true
	j.payloadTorn = false
	j.ackTorn = false
	if err := j.openFilesLocked(); err != nil {
		return err
	}
	if err := j.enforceDirBoundLocked(); err != nil {
		return err
	}
	return j.syncLocked()
}

func (j *Journal) recordTombstonesLocked(drop []*JournalEntry, retireRuns bool) error {
	if retireRuns {
		for _, e := range drop {
			if e == nil || !e.isEnd || e.endRun == "" {
				continue
			}
			if _, ok := j.fenced[e.endRun]; ok {
				continue
			}
			if err := j.addFenceLocked(e.endRun); err != nil {
				return err
			}
		}
	}
	var extra int64
	var lines [][]byte
	var add []tombstoneLine
	droppedPhys := int64(0)
	for _, e := range drop {
		if e == nil {
			continue
		}
		droppedPhys += e.encodedSize()
		if e.Acked {
			b, _ := json.Marshal(ackLine{MsgID: e.MsgID})
			droppedPhys += int64(len(b) + 1)
		}
		if _, ok := j.tomb[e.MsgID]; ok {
			continue
		}
		if run, ok := parseProtocolRun(e.MsgID); ok {
			if _, fenced := j.fenced[run]; fenced && retireRuns {
				continue
			}
		}
		ts := payloadHash(e.Subject, e.Payload)
		ts.MsgID = e.MsgID
		b, err := json.Marshal(ts)
		if err != nil {
			return err
		}
		extra += int64(len(b) + 1)
		lines = append(lines, b)
		add = append(add, ts)
	}
	projected := j.dirBytesLocked() - droppedPhys + extra
	if j.maxBytes > 0 && projected > j.boundLocked() {
		return fmt.Errorf("graphstream journal: identity metadata exceeds spool %d bytes", j.maxBytes)
	}
	for i, b := range lines {
		if err := j.writeRawLocked("tombstones.jsonl", b); err != nil {
			return err
		}
		j.tomb[add[i].MsgID] = add[i]
	}
	_ = os.Remove(filepath.Join(j.dir, "idents.bin"))
	return nil
}

func (j *Journal) addFenceLocked(runID string) error {
	if runID == "" {
		return nil
	}
	if _, ok := j.fenced[runID]; ok {
		return nil
	}
	line, err := json.Marshal(fenceLine{RunID: runID})
	if err != nil {
		return err
	}
	if err := j.writeRawLocked("fences.jsonl", line); err != nil {
		return err
	}
	j.fenced[runID] = struct{}{}
	return nil
}

func (j *Journal) enforceDirBoundLocked() error {
	if j.maxBytes <= 0 {
		return nil
	}
	if j.dirBytesLocked() <= j.boundLocked() {
		return nil
	}
	return fmt.Errorf("graphstream journal: directory exceeds spool %d bytes", j.maxBytes)
}

func (j *Journal) reclaimLiveLocked() error {
	var keep []string
	next := map[string]*JournalEntry{}
	var drop []*JournalEntry
	var unacked, waste int64
	for _, id := range j.order {
		e := j.byID[id]
		if !keepLive(e) {
			drop = append(drop, e)
			continue
		}
		keep = append(keep, id)
		next[id] = e
		n := e.encodedSize()
		if !e.Acked {
			unacked += n
		} else if !e.isEnd {
			waste += n
		}
	}
	if len(drop) == 0 {
		j.unackedBytes = unacked
		j.ackedWaste = waste
		return nil
	}
	if err := j.recordTombstonesLocked(drop, false); err != nil {
		return err
	}
	if err := j.flushDataLocked(); err != nil {
		return err
	}
	if err := j.closeFilesLocked(); err != nil {
		return err
	}
	if err := j.rewrite("payloads.jsonl", keep, next, false); err != nil {
		_ = j.openFilesLocked()
		return err
	}
	if err := j.rewriteAcks(keep, next, false); err != nil {
		_ = j.openFilesLocked()
		return err
	}
	if err := j.installRewritesLocked("payloads.jsonl", "acks.jsonl"); err != nil {
		_ = j.openFilesLocked()
		return err
	}
	var size int64
	for _, id := range keep {
		size += next[id].encodedSize()
		if next[id].Acked {
			b, _ := json.Marshal(ackLine{MsgID: id})
			size += int64(len(b) + 1)
		}
	}
	j.order = keep
	j.byID = next
	j.physical = size
	j.unackedBytes = unacked
	j.ackedWaste = waste
	j.unsynced = true
	j.payloadTorn = false
	j.ackTorn = false
	if err := j.openFilesLocked(); err != nil {
		return err
	}
	if err := j.enforceDirBoundLocked(); err != nil {
		return err
	}
	return j.syncLocked()
}

func (j *Journal) rewrite(name string, keep []string, byID map[string]*JournalEntry, install bool) error {
	path := filepath.Join(j.dir, name)
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	var written int64
	crc := uint64(0)
	for _, id := range keep {
		e := byID[id]
		b, err := json.Marshal(payloadLine{MsgID: e.MsgID, Subject: e.Subject, Payload: e.Payload})
		if err != nil {
			f.Close()
			os.Remove(tmp)
			return err
		}
		j.lineScratch = append(j.lineScratch[:0], b...)
		j.lineScratch = append(j.lineScratch, '\n')
		if _, err := w.Write(j.lineScratch); err != nil {
			f.Close()
			os.Remove(tmp)
			return err
		}
		crc = crc64.Update(crc, journalCRC, j.lineScratch)
		n := int64(len(b) + 1)
		written += n
		e.lineN = n
	}
	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	j.payloadValid = written
	j.payloadCRC = crc
	j.payloadNeedNL = false
	if !install {
		return nil
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := syncDir(j.dir); err != nil {
		return err
	}
	j.syncs++
	return nil
}

func (j *Journal) rewriteAcks(keep []string, byID map[string]*JournalEntry, install bool) error {
	path := filepath.Join(j.dir, "acks.jsonl")
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	var written int64
	crc := uint64(0)
	for _, id := range keep {
		e := byID[id]
		if e == nil || !e.Acked {
			continue
		}
		b, err := json.Marshal(ackLine{MsgID: e.MsgID})
		if err != nil {
			f.Close()
			os.Remove(tmp)
			return err
		}
		j.lineScratch = append(j.lineScratch[:0], b...)
		j.lineScratch = append(j.lineScratch, '\n')
		if _, err := w.Write(j.lineScratch); err != nil {
			f.Close()
			os.Remove(tmp)
			return err
		}
		crc = crc64.Update(crc, journalCRC, j.lineScratch)
		written += int64(len(b) + 1)
	}
	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	j.ackValid = written
	j.ackCRC = crc
	j.ackNeedNL = false
	if !install {
		return nil
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := syncDir(j.dir); err != nil {
		return err
	}
	j.syncs++
	return nil
}

func (j *Journal) writeEmptyAcksTmp() error {
	path := filepath.Join(j.dir, "acks.jsonl")
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	j.ackValid = 0
	j.ackCRC = 0
	j.ackTorn = false
	j.ackNeedNL = false
	return nil
}

func (j *Journal) installRewritesLocked(names ...string) error {
	if err := j.writeCommitNamedLocked("commit.next.json"); err != nil {
		return err
	}
	for _, name := range names {
		tmp := filepath.Join(j.dir, name+".tmp")
		path := filepath.Join(j.dir, name)
		if err := os.Rename(tmp, path); err != nil {
			return err
		}
	}
	if err := syncDir(j.dir); err != nil {
		return err
	}
	if err := j.writeCommitLocked(); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(j.dir, "commit.next.json"))
	return syncDir(j.dir)
}

func (j *Journal) rewriteTombsLocked(install bool) error {
	path := filepath.Join(j.dir, "tombstones.jsonl")
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	crc := uint64(0)
	var written int64
	next := map[string]tombstoneLine{}
	for id, ts := range j.tomb {
		if run, ok := parseProtocolRun(id); ok {
			if _, fenced := j.fenced[run]; fenced {
				continue
			}
		}
		b, err := json.Marshal(ts)
		if err != nil {
			f.Close()
			os.Remove(tmp)
			return err
		}
		j.lineScratch = append(j.lineScratch[:0], b...)
		j.lineScratch = append(j.lineScratch, '\n')
		if _, err := w.Write(j.lineScratch); err != nil {
			f.Close()
			os.Remove(tmp)
			return err
		}
		crc = crc64.Update(crc, journalCRC, j.lineScratch)
		written += int64(len(b) + 1)
		next[id] = ts
	}
	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	_ = os.Remove(filepath.Join(j.dir, "idents.bin"))
	if j.tombFile != nil {
		_ = j.tombBuf.Flush()
		_ = j.tombFile.Close()
		j.tombFile = nil
		j.tombBuf = nil
	}
	j.tomb = next
	j.tombValid = written
	j.tombBytes = written
	j.tombCRC = crc
	j.tombTorn = false
	j.tombNeedNL = false
	j.unsynced = true
	if !install {
		return nil
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := syncDir(j.dir); err != nil {
		return err
	}
	j.syncs++
	return nil
}

// Publisher writes through a journal then a sink. Publish is idempotent for a
// given MsgID: a replay of the same identity sends the stored payload.
type Publisher struct {
	Sink     Sink
	Journal  *Journal
	Subject  string
	MaxInFly int
	sem      chan struct{}
	async    *asyncPub
}

func (p *Publisher) acquire(ctx context.Context) error {
	n := p.MaxInFly
	if n <= 1 {
		return ctx.Err()
	}
	if p.sem == nil {
		p.sem = make(chan struct{}, n)
	}
	select {
	case p.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Publisher) release() {
	if p.sem != nil {
		<-p.sem
	}
}

// Publish delivers one payload. Duplicate IDs reuse the stored bytes.
//
// When EnableAsync is on, Publish only admits the payload into a bounded
// volatile in-memory queue. It does not take the journal mutex, check durable
// history, or wait on disk. A nil return is queue admission, not durability:
// a crash before Flush can lose queued jobs, and durable-history conflicts or
// retired-run errors surface from Flush. Queued byte conflicts still fail
// immediately. The commit worker validates history, appends new identities,
// and fsyncs before sink delivery. Flush waits until every admitted job is
// journal-durable and broker-acked (or a validation/delivery error). Do not
// checkpoint before Flush.
func (p *Publisher) Publish(ctx context.Context, msgID string, payload []byte) error {
	if err := p.acquire(ctx); err != nil {
		return err
	}
	defer p.release()
	if p.async != nil {
		return p.enqueue(ctx, msgID, payload)
	}
	if p.Journal != nil {
		if err := p.Journal.Append(JournalEntry{MsgID: msgID, Subject: p.Subject, Payload: payload}); err != nil {
			return err
		}
		if p.Journal.alreadyComplete(msgID) {
			return nil
		}
	}
	if err := p.Sink.Publish(ctx, p.Subject, msgID, payload); err != nil {
		return err
	}
	if p.Journal != nil {
		return p.Journal.Ack(msgID)
	}
	return nil
}

// ReplayUnacked re-sends unacked journal entries with identical payloads.
func (p *Publisher) ReplayUnacked(ctx context.Context) error {
	if p.Journal == nil {
		return nil
	}
	if err := p.Journal.Sync(); err != nil {
		return err
	}
	for _, e := range p.Journal.Unacked() {
		subj := e.Subject
		if subj == "" {
			subj = p.Subject
		}
		if err := p.Sink.Publish(ctx, subj, e.MsgID, e.Payload); err != nil {
			return err
		}
		if err := p.Journal.Ack(e.MsgID); err != nil {
			return err
		}
	}
	return nil
}

func (p *Publisher) Flush(ctx context.Context) error {
	if p.async != nil {
		if err := p.waitIdle(ctx); err != nil {
			return err
		}
	}
	if p.Journal != nil {
		if err := p.Journal.Sync(); err != nil {
			return err
		}
	}
	if p.Sink == nil {
		return nil
	}
	return p.Sink.Flush(ctx)
}
