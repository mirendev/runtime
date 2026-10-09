package query

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// StackCapture opts in to bounded best-effort eBPF stack capture.
type StackCapture struct {
	User        bool        `json:"user,omitempty"`
	Kernel      bool        `json:"kernel,omitempty"`
	Depth       int         `json:"depth,omitempty"` // default 32, maximum 64
	Symbolize   bool        `json:"symbolize,omitempty"`
	UserShape   *StackShape `json:"user_shape,omitempty"`
	KernelShape *StackShape `json:"kernel_shape,omitempty"`
}

// StackShape projects leaf-first frames into aggregation keys, without changing
// raw event frames. Order: DropBottom, DropTop, From, Until, Top, DropOffsets.
type StackShape struct {
	DropOffsets  bool     `json:"drop_offsets,omitempty"`
	DropBottom   int      `json:"drop_bottom,omitempty"`
	DropTop      int      `json:"drop_top,omitempty"`
	Top          int      `json:"top,omitempty"`           // zero keeps all remaining frames
	Until        string   `json:"until,omitempty"`         // exact function name or one edge glob; matching frame is included
	From         string   `json:"from,omitempty"`          // discard leaf-side frames before the first match, inclusive match retained
	FromPatterns []string `json:"from_patterns,omitempty"` // alternative to From; first matching frame wins, not pattern order
}

type CapturedStack struct {
	Frames            []SymbolFrame `json:"frames,omitempty"`
	Error             string        `json:"error,omitempty"`
	CaptureErrorCode  int64         `json:"capture_error_code,omitempty"`  // signed bpf_get_stackid result, not a symbolization error
	DepthLimitReached bool          `json:"depth_limit_reached,omitempty"` // possibly truncated; exact-depth complete stacks are indistinguishable
}

// StackCoverage describes matching received events before shaping and output limits.
// Kernel-wide failures (including undelivered events) remain in CollectionStats.
type StackCoverage struct {
	Events                uint64            `json:"events"`
	Captured              uint64            `json:"captured"` // at least one raw frame, even if symbolization failed
	Missing               uint64            `json:"missing"`
	Empty                 uint64            `json:"empty"`
	CaptureFailures       uint64            `json:"capture_failures"`
	HelperErrors          map[string]uint64 `json:"helper_errors,omitempty"` // signed decimal error codes
	LookupFailures        uint64            `json:"lookup_failures"`
	DepthLimitReached     uint64            `json:"depth_limit_reached"`
	Frames                uint64            `json:"frames"`
	NamedFrames           uint64            `json:"named_frames"`
	UnresolvedFrames      uint64            `json:"unresolved_frames"`
	FullySymbolized       uint64            `json:"fully_symbolized"`
	PartiallySymbolized   uint64            `json:"partially_symbolized"`
	Unsymbolized          uint64            `json:"unsymbolized"`
	SymbolizationFailures uint64            `json:"symbolization_failures"` // stack-level symbolizer errors; frame gaps are counted separately
	SymbolizationDisabled uint64            `json:"symbolization_disabled"`
}

type StackCoverageReport struct {
	User   *StackCoverage `json:"user,omitempty"`
	Kernel *StackCoverage `json:"kernel,omitempty"`
}

func (c *StackCoverage) observe(stack *CapturedStack, symbolize bool) {
	c.Events++
	if stack == nil {
		c.Missing++
		return
	}
	if len(stack.Frames) == 0 {
		if stack.CaptureErrorCode != 0 || stack.Error != "" {
			c.CaptureFailures++
			if stack.CaptureErrorCode != 0 {
				if c.HelperErrors == nil {
					c.HelperErrors = make(map[string]uint64)
				}
				c.HelperErrors[fmt.Sprint(stack.CaptureErrorCode)]++
			} else {
				c.LookupFailures++
			}
		} else {
			c.Empty++
		}
		return
	}
	c.Captured++
	c.Frames += uint64(len(stack.Frames))
	if stack.DepthLimitReached {
		c.DepthLimitReached++
	}
	if !symbolize {
		c.SymbolizationDisabled++
		return
	}
	if stack.Error != "" {
		c.SymbolizationFailures++
	}
	var named uint64
	for _, frame := range stack.Frames {
		if frame.Name != "" {
			named++
		}
	}
	c.NamedFrames += named
	c.UnresolvedFrames += uint64(len(stack.Frames)) - named
	switch {
	case named == uint64(len(stack.Frames)):
		c.FullySymbolized++
	case named == 0:
		c.Unsymbolized++
	default:
		c.PartiallySymbolized++
	}
}

func (s StackCapture) Validate() error {
	if (!s.User && !s.Kernel) || s.Depth < 0 || s.Depth > 64 {
		return errors.New("stack capture requires user/kernel and depth between 0 and 64")
	}
	for _, option := range []struct {
		enabled bool
		shape   *StackShape
	}{{s.User, s.UserShape}, {s.Kernel, s.KernelShape}} {
		if option.shape == nil {
			continue
		}
		shape := option.shape
		if !option.enabled {
			return errors.New("stack shaping requires capture of that stack")
		}
		if shape.Top < 0 || shape.Top > 64 || shape.DropBottom < 0 || shape.DropBottom > 64 || shape.DropTop < 0 || shape.DropTop > 64 {
			return errors.New("stack top/drop_bottom/drop_top must be between 0 and 64")
		}
		if len(shape.FromPatterns) > 16 || (shape.From != "" && len(shape.FromPatterns) != 0) || slices.Contains(shape.FromPatterns, "") {
			return errors.New("stack from accepts 1–16 nonempty patterns, not both from and from_patterns")
		}
		for _, pattern := range append([]string{shape.Until, shape.From}, shape.FromPatterns...) {
			// Only edge stars are wildcards. Interior stars (notably Go
			// pointer receivers like os.(*File).Sync) are literal characters.
			if pattern != "" && (!s.Symbolize || pattern == "*" || (strings.HasPrefix(pattern, "*") && strings.HasSuffix(pattern, "*")) || len(pattern) > 256) {
				return errors.New("stack until/from requires symbolization and an exact name or one edge glob (up to 256 bytes)")
			}
		}
	}
	return nil
}

func (s StackCapture) depth() int {
	if s.Depth == 0 {
		return 32
	}
	return s.Depth
}

func (s CapturedStack) key(shape *StackShape) string {
	selected := s.Frames
	if shape != nil {
		selected = selected[:max(0, len(selected)-shape.DropBottom)]
		selected = selected[min(shape.DropTop, len(selected)):]
		if shape.From != "" || len(shape.FromPatterns) != 0 {
			for i, frame := range selected {
				if frame.Name != "" && ((shape.From != "" && processNameMatches(shape.From, frame.Name)) || slices.ContainsFunc(shape.FromPatterns, func(pattern string) bool { return processNameMatches(pattern, frame.Name) })) {
					selected = selected[i:]
					break
				}
			}
		}
		if shape.Until != "" {
			for i, frame := range selected {
				if frame.Name != "" && processNameMatches(shape.Until, frame.Name) {
					selected = selected[:i+1]
					break
				}
			}
		}
		if shape.Top > 0 && len(selected) > shape.Top {
			selected = selected[:shape.Top]
		}
	}
	frames := make([]string, 0, len(selected)+1)
	for _, frame := range selected {
		if frame.Name != "" {
			name := frame.Module + ":" + frame.Name
			if shape == nil || !shape.DropOffsets {
				name += fmt.Sprintf("+0x%x", frame.Offset)
			}
			frames = append(frames, escapeStackFrame(name))
		} else if frame.Module != "" && frame.FileOffset != nil {
			frames = append(frames, escapeStackFrame(fmt.Sprintf("%s@file+0x%x", frame.Module, *frame.FileOffset)))
		} else {
			frames = append(frames, escapeStackFrame(frame.Address))
		}
	}
	if s.Error != "" {
		frames = append(frames, escapeStackFrame("["+s.Error+"]"))
	}
	return strings.Join(frames, ";")
}

// Escape only frame separators/control characters, leaving ordinary symbol
// names readable. Percent is escaped first so encoding cannot create collisions.
var stackFrameEscaper = strings.NewReplacer("%", "%25", ";", "%3B", "\n", "%0A", "\r", "%0D", "\t", "%09")

func escapeStackFrame(frame string) string {
	return stackFrameEscaper.Replace(frame)
}
