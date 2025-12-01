package lsvd

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"os"
	"time"
)

// TortureBlockHash is a compact representation of block contents for verification
type TortureBlockHash [8]byte

func HashBlock(data []byte) TortureBlockHash {
	h := sha256.Sum256(data)
	var bh TortureBlockHash
	copy(bh[:], h[:8])
	return bh
}

// TortureDiskModel is a simple reference model tracking expected disk state
type TortureDiskModel struct {
	blocks   map[LBA]TortureBlockHash
	writeSeq uint64
}

func NewTortureDiskModel() *TortureDiskModel {
	return &TortureDiskModel{
		blocks: make(map[LBA]TortureBlockHash),
	}
}

func (m *TortureDiskModel) WriteExtent(lba LBA, data []byte) {
	m.writeSeq++
	blocks := uint32(len(data) / BlockSize)
	for i := uint32(0); i < blocks; i++ {
		blockData := data[i*BlockSize : (i+1)*BlockSize]
		m.blocks[lba+LBA(i)] = HashBlock(blockData)
	}
}

func (m *TortureDiskModel) ZeroBlocks(lba LBA, blocks uint32) {
	m.writeSeq++
	zeroHash := HashBlock(make([]byte, BlockSize))
	for i := uint32(0); i < blocks; i++ {
		m.blocks[lba+LBA(i)] = zeroHash
	}
}

func (m *TortureDiskModel) ExpectedHash(lba LBA) (TortureBlockHash, bool) {
	h, ok := m.blocks[lba]
	return h, ok
}

func (m *TortureDiskModel) WrittenLBAs() []LBA {
	lbas := make([]LBA, 0, len(m.blocks))
	for lba := range m.blocks {
		lbas = append(lbas, lba)
	}
	return lbas
}

func (m *TortureDiskModel) BlockCount() int {
	return len(m.blocks)
}

func (m *TortureDiskModel) HasBlock(lba LBA) bool {
	_, ok := m.blocks[lba]
	return ok
}

// TortureOpType represents a type of operation in the torture test
type TortureOpType int

const (
	TortureOpWrite TortureOpType = iota
	TortureOpRead
	TortureOpZero
	TortureOpSync
	TortureOpCloseReopen
)

func (o TortureOpType) String() string {
	switch o {
	case TortureOpWrite:
		return "write"
	case TortureOpRead:
		return "read"
	case TortureOpZero:
		return "zero"
	case TortureOpSync:
		return "sync"
	case TortureOpCloseReopen:
		return "close"
	default:
		return "unknown"
	}
}

// TortureDataPattern represents a data pattern for write operations
type TortureDataPattern int

const (
	TorturePatternRandom TortureDataPattern = iota
	TorturePatternZero
	TorturePatternCompressible
	TorturePatternSequential
)

// TortureOperation represents a single operation in the torture test
type TortureOperation struct {
	Type     TortureOpType
	Extent   Extent
	DataSeed int64
	Pattern  TortureDataPattern
}

func (o TortureOperation) String() string {
	switch o.Type {
	case TortureOpWrite:
		return fmt.Sprintf("write  LBA:%-8d Blocks:%-4d seed:%d", o.Extent.LBA, o.Extent.Blocks, o.DataSeed)
	case TortureOpRead:
		return fmt.Sprintf("read   LBA:%-8d Blocks:%-4d", o.Extent.LBA, o.Extent.Blocks)
	case TortureOpZero:
		return fmt.Sprintf("zero   LBA:%-8d Blocks:%-4d", o.Extent.LBA, o.Extent.Blocks)
	case TortureOpSync:
		return "sync"
	case TortureOpCloseReopen:
		return "close/reopen"
	default:
		return "unknown"
	}
}

// TortureOpWeights defines the probability weights for each operation type
type TortureOpWeights struct {
	Write       int `json:"Write"`
	Read        int `json:"Read"`
	Zero        int `json:"Zero"`
	Sync        int `json:"Sync"`
	CloseReopen int `json:"CloseReopen"`
}

// DefaultTortureWeights provides sensible defaults for torture testing
var DefaultTortureWeights = TortureOpWeights{
	Write:       50,
	Read:        30,
	Zero:        10,
	Sync:        5,
	CloseReopen: 5,
}

// TortureConfig contains all configuration for a torture test run
type TortureConfig struct {
	Seed               int64            `json:"Seed"`
	Operations         int              `json:"Operations"`
	MaxLBA             LBA              `json:"MaxLBA"`
	MaxBlocks          uint32           `json:"MaxBlocks"`
	Weights            TortureOpWeights `json:"Weights"`
	OverlapProbability float64          `json:"OverlapProbability"`
	VerifyEvery        int              `json:"VerifyEvery"`
	PatternWeights     [4]int           `json:"PatternWeights"` // random, zero, compressible, sequential
}

// DefaultTortureConfig provides a sensible default configuration
var DefaultTortureConfig = TortureConfig{
	Operations:         10000,
	MaxLBA:             100000,
	MaxBlocks:          64,
	Weights:            DefaultTortureWeights,
	OverlapProbability: 0.3,
	VerifyEvery:        1000,
	PatternWeights:     [4]int{60, 10, 20, 10},
}

// EncodeTortureConfig encodes a TortureConfig to a base64 JSON string for reproduction
func EncodeTortureConfig(cfg TortureConfig) string {
	data, err := json.Marshal(cfg)
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(data)
}

// DecodeTortureConfig decodes a base64 JSON string back to a TortureConfig
func DecodeTortureConfig(encoded string) (TortureConfig, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return TortureConfig{}, fmt.Errorf("base64 decode failed: %w", err)
	}
	var cfg TortureConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return TortureConfig{}, fmt.Errorf("json unmarshal failed: %w", err)
	}
	return cfg, nil
}

// GenerateTortureData generates deterministic data from a seed and pattern
func GenerateTortureData(rng *rand.Rand, pattern TortureDataPattern, blocks uint32) []byte {
	data := make([]byte, blocks*BlockSize)

	switch pattern {
	case TorturePatternRandom:
		rng.Read(data)
	case TorturePatternZero:
		// Already zero
	case TorturePatternCompressible:
		pat := make([]byte, 64)
		rng.Read(pat)
		for i := 0; i < len(data); i += 64 {
			copy(data[i:], pat)
		}
	case TorturePatternSequential:
		for i := range data {
			data[i] = byte((i / BlockSize) ^ (i % 256))
		}
	}

	return data
}

// TortureGenerator generates deterministic operation sequences
type TortureGenerator struct {
	rng          *rand.Rand
	cfg          TortureConfig
	lastWrite    Extent
	totalWeight  int
	patternTotal int
}

func NewTortureGenerator(cfg TortureConfig) *TortureGenerator {
	g := &TortureGenerator{
		rng: rand.New(rand.NewSource(cfg.Seed)),
		cfg: cfg,
	}
	g.totalWeight = cfg.Weights.Write + cfg.Weights.Read + cfg.Weights.Zero +
		cfg.Weights.Sync + cfg.Weights.CloseReopen
	g.patternTotal = cfg.PatternWeights[0] + cfg.PatternWeights[1] +
		cfg.PatternWeights[2] + cfg.PatternWeights[3]
	return g
}

func (g *TortureGenerator) nextOpType() TortureOpType {
	w := g.cfg.Weights
	choice := g.rng.Intn(g.totalWeight)

	switch {
	case choice < w.Write:
		return TortureOpWrite
	case choice < w.Write+w.Read:
		return TortureOpRead
	case choice < w.Write+w.Read+w.Zero:
		return TortureOpZero
	case choice < w.Write+w.Read+w.Zero+w.Sync:
		return TortureOpSync
	default:
		return TortureOpCloseReopen
	}
}

func (g *TortureGenerator) nextExtent(allowOverlap bool) Extent {
	var lba LBA
	var blocks uint32

	boundaryChance := g.rng.Float64()
	switch {
	case boundaryChance < 0.02:
		lba = 0
	case boundaryChance < 0.04:
		lba = g.cfg.MaxLBA - LBA(g.cfg.MaxBlocks)
	case boundaryChance < 0.06:
		segmentBlocks := LBA(8192)
		lba = segmentBlocks - LBA(g.rng.Intn(10))
	default:
		lba = LBA(g.rng.Int63n(int64(g.cfg.MaxLBA)))
	}

	blockChance := g.rng.Float64()
	switch {
	case blockChance < 0.4:
		blocks = 1
	case blockChance < 0.7:
		blocks = uint32(g.rng.Intn(4)) + 1
	case blockChance < 0.9:
		blocks = uint32(g.rng.Intn(16)) + 1
	case blockChance < 0.98:
		blocks = uint32(g.rng.Intn(int(g.cfg.MaxBlocks))) + 1
	default:
		blocks = g.cfg.MaxBlocks
	}

	if allowOverlap && g.lastWrite.Blocks > 0 && g.rng.Float64() < g.cfg.OverlapProbability {
		overlapType := g.rng.Intn(5)
		switch overlapType {
		case 0:
			if g.lastWrite.Blocks > 1 {
				overlap := uint32(g.rng.Intn(int(g.lastWrite.Blocks/2))) + 1
				lba = g.lastWrite.LBA + LBA(g.lastWrite.Blocks) - LBA(overlap)
			}
		case 1:
			if blocks > 1 {
				overlap := uint32(g.rng.Intn(int(blocks/2))) + 1
				if g.lastWrite.LBA >= LBA(blocks-overlap) {
					lba = g.lastWrite.LBA - LBA(blocks-overlap)
				}
			}
		case 2:
			lba = g.lastWrite.LBA
			blocks = g.lastWrite.Blocks
		case 3:
			if g.lastWrite.Blocks > 2 {
				lba = g.lastWrite.LBA + 1
				blocks = g.lastWrite.Blocks - 2
			}
		case 4:
			if g.lastWrite.LBA > 0 {
				lba = g.lastWrite.LBA - 1
				blocks = g.lastWrite.Blocks + 2
			}
		}
	}

	if lba >= g.cfg.MaxLBA {
		lba = g.cfg.MaxLBA - 1
	}
	if lba+LBA(blocks) > g.cfg.MaxLBA {
		blocks = uint32(g.cfg.MaxLBA - lba)
	}
	if blocks == 0 {
		blocks = 1
	}

	return Extent{LBA: lba, Blocks: blocks}
}

func (g *TortureGenerator) nextPattern() TortureDataPattern {
	choice := g.rng.Intn(g.patternTotal)
	pw := g.cfg.PatternWeights

	switch {
	case choice < pw[0]:
		return TorturePatternRandom
	case choice < pw[0]+pw[1]:
		return TorturePatternZero
	case choice < pw[0]+pw[1]+pw[2]:
		return TorturePatternCompressible
	default:
		return TorturePatternSequential
	}
}

func (g *TortureGenerator) Next() TortureOperation {
	opType := g.nextOpType()

	op := TortureOperation{Type: opType}

	switch opType {
	case TortureOpWrite:
		op.Extent = g.nextExtent(true)
		op.DataSeed = g.rng.Int63()
		op.Pattern = g.nextPattern()
		g.lastWrite = op.Extent
	case TortureOpRead:
		op.Extent = g.nextExtent(false)
	case TortureOpZero:
		op.Extent = g.nextExtent(true)
		g.lastWrite = op.Extent
	}

	return op
}

// TortureRunner runs torture tests against an LSVD disk
type TortureRunner struct {
	top     context.Context
	cfg     TortureConfig
	gen     *TortureGenerator
	model   *TortureDiskModel
	disk    *Disk
	ctx     *Context
	log     *slog.Logger
	tmpDir  string
	history []TortureOperation
	output  io.Writer

	opCount      int
	lastProgress time.Time
}

// TortureResult contains the result of a torture test run
type TortureResult struct {
	Success    bool
	Operations int
	LBAsUsed   int
	Error      error
	History    []TortureOperation
}

// NewTortureRunner creates a new torture test runner
func NewTortureRunner(gctx context.Context, log *slog.Logger, tmpDir string, cfg TortureConfig) (*TortureRunner, error) {
	if tmpDir == "" {
		var err error
		tmpDir, err = os.MkdirTemp("", "lsvd-torture-*")
		if err != nil {
			return nil, fmt.Errorf("failed to create temp dir: %w", err)
		}
	}

	ctx := NewContext(gctx)

	disk, err := NewDisk(ctx, log, tmpDir)
	if err != nil {
		ctx.Close()
		return nil, fmt.Errorf("failed to create disk: %w", err)
	}

	return &TortureRunner{
		top:     gctx,
		cfg:     cfg,
		gen:     NewTortureGenerator(cfg),
		model:   NewTortureDiskModel(),
		disk:    disk,
		ctx:     ctx,
		log:     log,
		tmpDir:  tmpDir,
		history: make([]TortureOperation, 0, cfg.Operations),
		output:  os.Stderr,
	}, nil
}

// SetOutput sets the output writer for progress messages
func (r *TortureRunner) SetOutput(w io.Writer) {
	r.output = w
}

// Cleanup cleans up resources used by the runner
func (r *TortureRunner) Cleanup() {
	if r.disk != nil {
		r.disk.Close(r.ctx)
		r.disk = nil
	}
	if r.ctx != nil {
		r.ctx.Close()
		r.ctx = nil
	}
	if r.tmpDir != "" {
		os.RemoveAll(r.tmpDir)
		r.tmpDir = ""
	}
}

// Run executes the torture test
func (r *TortureRunner) Run() TortureResult {
	r.lastProgress = time.Now()

	for i := 0; i < r.cfg.Operations; i++ {
		op := r.gen.Next()
		r.history = append(r.history, op)
		r.opCount = i + 1

		if err := r.executeOp(op); err != nil {
			return TortureResult{
				Success:    false,
				Operations: i,
				LBAsUsed:   r.model.BlockCount(),
				Error:      fmt.Errorf("operation %d failed: %w", i, err),
				History:    r.history,
			}
		}

		if r.cfg.VerifyEvery > 0 && (i+1)%r.cfg.VerifyEvery == 0 {
			if err := r.verifyAll(); err != nil {
				return TortureResult{
					Success:    false,
					Operations: i + 1,
					LBAsUsed:   r.model.BlockCount(),
					Error:      fmt.Errorf("verification at op %d failed: %w", i, err),
					History:    r.history,
				}
			}
		}

		r.ctx.Reset()

		// Progress reporting every 5 seconds
		if time.Since(r.lastProgress) > 5*time.Second {
			select {
			case <-r.ctx.Done():
				fmt.Fprintf(r.output, "Torture test cancelled after %d operations\n", i+1)
				return TortureResult{
					Success:    false,
					Operations: i + 1,
					LBAsUsed:   r.model.BlockCount(),
					Error:      fmt.Errorf("torture test cancelled"),
					History:    r.history,
				}
			default:
				fmt.Fprintf(r.output, "Progress: %d/%d operations (%.1f%%)\n",
					i+1, r.cfg.Operations, float64(i+1)/float64(r.cfg.Operations)*100)
				r.lastProgress = time.Now()
			}
		}
	}

	// Final verification
	if err := r.verifyAll(); err != nil {
		return TortureResult{
			Success:    false,
			Operations: r.cfg.Operations,
			LBAsUsed:   r.model.BlockCount(),
			Error:      fmt.Errorf("final verification failed: %w", err),
			History:    r.history,
		}
	}

	return TortureResult{
		Success:    true,
		Operations: r.cfg.Operations,
		LBAsUsed:   r.model.BlockCount(),
		History:    r.history,
	}
}

func (r *TortureRunner) executeOp(op TortureOperation) error {
	switch op.Type {
	case TortureOpWrite:
		return r.execWrite(op)
	case TortureOpRead:
		return r.execRead(op)
	case TortureOpZero:
		return r.execZero(op)
	case TortureOpSync:
		return r.execSync()
	case TortureOpCloseReopen:
		return r.execCloseReopen()
	default:
		return fmt.Errorf("unknown operation type: %d", op.Type)
	}
}

func (r *TortureRunner) execWrite(op TortureOperation) error {
	dataRng := rand.New(rand.NewSource(op.DataSeed))
	data := GenerateTortureData(dataRng, op.Pattern, op.Extent.Blocks)

	r.model.WriteExtent(op.Extent.LBA, data)

	rd := MapRangeData(op.Extent, data)
	return r.disk.WriteExtent(r.ctx, rd)
}

func (r *TortureRunner) execRead(op TortureOperation) error {
	defer r.ctx.Reset()

	actual, err := r.disk.ReadExtent(r.ctx, op.Extent)
	if err != nil {
		return fmt.Errorf("read error: %w", err)
	}

	actualData := actual.ReadData()
	zeroHash := HashBlock(make([]byte, BlockSize))

	for i := uint32(0); i < op.Extent.Blocks; i++ {
		lba := op.Extent.LBA + LBA(i)
		blockData := actualData[i*BlockSize : (i+1)*BlockSize]
		actualHash := HashBlock(blockData)
		expectedHash, modelHas := r.model.ExpectedHash(lba)
		if !modelHas {
			expectedHash = zeroHash
		}

		if lba == 14654 {
			fmt.Printf("Debug LBA 14654: expected %s, got %s, modelHas=%v\n",
				hex.EncodeToString(expectedHash[:]), hex.EncodeToString(actualHash[:]), modelHas)
		}

		if actualHash != expectedHash {
			isExpectedZero := expectedHash == zeroHash
			isActualZero := tortureIsEmpty(blockData)

			firstNonZero := -1
			for k, b := range blockData {
				if b != 0 {
					firstNonZero = k
					break
				}
			}

			var relevantWrites []string
			for idx, histOp := range r.history {
				if histOp.Type == TortureOpWrite || histOp.Type == TortureOpZero {
					start := histOp.Extent.LBA
					end := start + LBA(histOp.Extent.Blocks)
					if lba >= start && lba < end {
						relevantWrites = append(relevantWrites, fmt.Sprintf("op[%d]:%s", idx, histOp))
					}
				}
			}

			// Debug: retry reads to detect timing issues
			retryHash1 := "read-failed"
			retryHash2 := "read-failed"
			r.ctx.Reset()
			if singleBlock, err := r.disk.ReadExtent(r.ctx, Extent{LBA: lba, Blocks: 1}); err == nil {
				h := HashBlock(singleBlock.ReadData())
				retryHash1 = hex.EncodeToString(h[:])
			}
			r.ctx.Reset()
			if retryExtent, err := r.disk.ReadExtent(r.ctx, op.Extent); err == nil {
				retryData := retryExtent.ReadData()
				retryBlockData := retryData[i*BlockSize : (i+1)*BlockSize]
				h := HashBlock(retryBlockData)
				retryHash2 = hex.EncodeToString(h[:])
			}

			return fmt.Errorf("data mismatch at LBA %d: expected %s, got %s (modelHas=%v, expectZero=%v, actualZero=%v, firstNonZero=%d, retrySingle=%s, retryExtent=%s, relevantWrites=%v)",
				lba, hex.EncodeToString(expectedHash[:]), hex.EncodeToString(actualHash[:]),
				modelHas, isExpectedZero, isActualZero, firstNonZero, retryHash1, retryHash2, relevantWrites)
		}
	}

	return nil
}

func (r *TortureRunner) execZero(op TortureOperation) error {
	r.model.ZeroBlocks(op.Extent.LBA, op.Extent.Blocks)
	return r.disk.ZeroBlocks(r.ctx, op.Extent)
}

func (r *TortureRunner) execSync() error {
	return r.disk.SyncWriteCache()
}

func (r *TortureRunner) execCloseReopen() error {
	if err := r.disk.Close(r.ctx); err != nil {
		return fmt.Errorf("close error: %w", err)
	}

	disk, err := NewDisk(r.ctx, r.log, r.tmpDir)
	if err != nil {
		return fmt.Errorf("reopen error: %w", err)
	}
	r.disk = disk

	return r.verifySample(100)
}

func (r *TortureRunner) verifyAll() error {
	lbas := r.model.WrittenLBAs()
	if len(lbas) == 0 {
		return nil
	}

	for _, lba := range lbas {
		r.ctx.Reset()

		actual, err := r.disk.ReadExtent(r.ctx, Extent{LBA: lba, Blocks: 1})
		if err != nil {
			return fmt.Errorf("read error at LBA %d: %w", lba, err)
		}

		actualHash := HashBlock(actual.ReadData())
		expectedHash, _ := r.model.ExpectedHash(lba)

		if actualHash != expectedHash {
			return fmt.Errorf("verification failed at LBA %d: expected %s, got %s",
				lba, hex.EncodeToString(expectedHash[:]), hex.EncodeToString(actualHash[:]))
		}
	}

	return nil
}

func (r *TortureRunner) verifySample(count int) error {
	lbas := r.model.WrittenLBAs()
	if len(lbas) == 0 {
		return nil
	}

	if len(lbas) <= count {
		return r.verifyAll()
	}

	sampled := make(map[int]bool)
	for len(sampled) < count {
		idx := r.gen.rng.Intn(len(lbas))
		sampled[idx] = true
	}

	for idx := range sampled {
		r.ctx.Reset()

		lba := lbas[idx]
		actual, err := r.disk.ReadExtent(r.ctx, Extent{LBA: lba, Blocks: 1})
		if err != nil {
			return fmt.Errorf("read error at LBA %d: %w", lba, err)
		}

		actualHash := HashBlock(actual.ReadData())
		expectedHash, _ := r.model.ExpectedHash(lba)

		if actualHash != expectedHash {
			return fmt.Errorf("sample verification failed at LBA %d: expected %s, got %s",
				lba, hex.EncodeToString(expectedHash[:]), hex.EncodeToString(actualHash[:]))
		}
	}

	return nil
}

// DumpHistory writes the last N operations to the output writer
func (r *TortureRunner) DumpHistory(last int) {
	fmt.Fprintln(r.output, "--- Operation History (last operations) ---")
	start := 0
	if len(r.history) > last {
		start = len(r.history) - last
	}
	for i := start; i < len(r.history); i++ {
		marker := "  "
		if i == len(r.history)-1 {
			marker = "→ "
		}
		fmt.Fprintf(r.output, "%s[%5d] %s\n", marker, i, r.history[i])
	}
}

// DumpHistoryRange prints operations in the range [start, end)
func (r *TortureRunner) DumpHistoryRange(start, end int) {
	if start < 0 {
		start = 0
	}
	if end > len(r.history) {
		end = len(r.history)
	}
	if start >= end {
		fmt.Fprintf(r.output, "--- No operations in range [%d, %d) ---\n", start, end)
		return
	}
	fmt.Fprintf(r.output, "--- Operation History [%d, %d) ---\n", start, end)
	for i := start; i < end; i++ {
		fmt.Fprintf(r.output, "  [%5d] %s\n", i, r.history[i])
	}
}

// tortureIsEmpty checks if all bytes in the slice are zero
func tortureIsEmpty(d []byte) bool {
	for _, b := range d {
		if b != 0 {
			return false
		}
	}
	return true
}

// TortureVariation defines a named torture test configuration variation
type TortureVariation struct {
	Name    string
	Weights TortureOpWeights
	Overlap float64
	MaxLBA  LBA
}

// DefaultTortureVariations returns the standard set of torture test variations
func DefaultTortureVariations() []TortureVariation {
	return []TortureVariation{
		{"default", DefaultTortureWeights, 0.3, 100000},
		{"no-close-reopen", TortureOpWeights{Write: 50, Read: 35, Zero: 10, Sync: 5, CloseReopen: 0}, 0.3, 100000},
		{"high-overlap", TortureOpWeights{Write: 70, Read: 30, Zero: 0, Sync: 0, CloseReopen: 0}, 0.6, 100000},
		{"heavy-zero", TortureOpWeights{Write: 40, Read: 30, Zero: 25, Sync: 5, CloseReopen: 0}, 0.3, 100000},
		{"durability", TortureOpWeights{Write: 40, Read: 30, Zero: 5, Sync: 10, CloseReopen: 15}, 0.3, 100000},
		{"boundaries", DefaultTortureWeights, 0.5, 1000},
	}
}
