package stackbuild

import (
	"bufio"
	"bytes"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"miren.dev/runtime/pkg/imagerefs"
)

// elixirVersionRe matches the versions people write for Elixir: a minor or a
// full version, optionally with asdf's OTP suffix ("1.18", "1.18.4",
// "1.18.4-otp-27").
var elixirVersionRe = regexp.MustCompile(`^(\d+\.\d+)(?:\.(\d+))?(?:-otp-(\d+))?$`)

// otpMajorRe takes the major from an Erlang version ("27", "27.3.4").
var otpMajorRe = regexp.MustCompile(`^(\d+)(?:\.|$)`)

// elixirPin is an Elixir (and maybe Erlang) version an app pins in a version
// manager's file.
type elixirPin struct {
	elixir string // as written, e.g. "1.18.4-otp-27"
	erlang string // as written, e.g. "27.3.4"; may be empty
	source string // the file it came from
}

// elixirBuild is the image an Elixir build resolved to.
type elixirBuild struct {
	tag    string
	detail string // human-readable: what was asked for and what it maps to
}

// resolveElixirVersion maps an Elixir version and optional OTP major onto a
// tag from imagerefs.ElixirReleases.
func resolveElixirVersion(version, otpMajor string) (elixirBuild, error) {
	m := elixirVersionRe.FindStringSubmatch(version)
	if m == nil {
		return elixirBuild{}, fmt.Errorf("unrecognized Elixir version %q: use a version like 1.18 or 1.18-otp-27, or a full hexpm/elixir tag", version)
	}
	minor, patch := m[1], m[2]
	if m[3] != "" {
		otpMajor = m[3]
	}

	rel, ok := imagerefs.ElixirReleases[minor]
	if !ok {
		return elixirBuild{}, fmt.Errorf("no Miren build for Elixir %s (available: %s): pick one of those, or set build.version to a full hexpm/elixir tag", minor, strings.Join(slices.Sorted(maps.Keys(imagerefs.ElixirReleases)), ", "))
	}
	if otpMajor == "" {
		otpMajor = rel.DefaultOTP
	}
	if _, ok := rel.OTP[otpMajor]; !ok {
		return elixirBuild{}, fmt.Errorf("no Miren build for Elixir %s on OTP %s (available: OTP %s): pick one of those, or set build.version to a full hexpm/elixir tag", minor, otpMajor, strings.Join(slices.Sorted(maps.Keys(rel.OTP)), ", "))
	}

	detail := fmt.Sprintf("Elixir %s on OTP %s", rel.Patch, rel.OTP[otpMajor])
	if patch != "" && minor+"."+patch != rel.Patch {
		detail += fmt.Sprintf(" (the %s release Miren builds, for %s)", minor, version)
	}
	return elixirBuild{tag: imagerefs.ElixirTag(minor, otpMajor), detail: detail}, nil
}

// readElixirPin looks for a pinned Elixir version where Elixir projects
// usually keep one: asdf's .tool-versions, then mise's config.
func (s *ElixirStack) readElixirPin() *elixirPin {
	if content, err := s.readFile(".tool-versions"); err == nil {
		tools := toolVersions(content)
		if tools["elixir"] != "" {
			return &elixirPin{elixir: tools["elixir"], erlang: tools["erlang"], source: ".tool-versions"}
		}
	}
	for _, name := range []string{"mise.toml", ".mise.toml"} {
		content, err := s.readFile(name)
		if err != nil {
			continue
		}
		var cfg struct {
			Tools map[string]any `toml:"tools"`
		}
		if toml.Unmarshal(content, &cfg) != nil {
			continue
		}
		if elixir := miseVersion(cfg.Tools["elixir"]); elixir != "" {
			return &elixirPin{elixir: elixir, erlang: miseVersion(cfg.Tools["erlang"]), source: name}
		}
	}
	return nil
}

// toolVersions parses an asdf .tool-versions file into tool -> version. When a
// tool lists several versions, the first is the one asdf uses.
func toolVersions(content []byte) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(content))
	for sc.Scan() {
		line, _, _ := strings.Cut(sc.Text(), "#")
		if fields := strings.Fields(line); len(fields) >= 2 {
			out[fields[0]] = fields[1]
		}
	}
	return out
}

// miseVersion reads a mise tool entry, which can be a string, a list of
// versions (the first wins), or a table with a version key.
func miseVersion(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case []any:
		if len(v) > 0 {
			return miseVersion(v[0])
		}
	case map[string]any:
		return miseVersion(v["version"])
	}
	return ""
}

// otpMajor returns the major version from an Erlang version, or "".
func otpMajor(erlang string) string {
	if m := otpMajorRe.FindStringSubmatch(erlang); m != nil {
		return m[1]
	}
	return ""
}

// builderImage picks the hexpm/elixir tag to build on: build.version when set
// (a version like 1.18, or a full tag as the escape hatch), else a version the
// app pins in .tool-versions or mise.toml, else the default.
func (s *ElixirStack) builderImage(opts BuildOptions) (elixirBuild, error) {
	if v := opts.Version; v != "" {
		if strings.Contains(v, "-erlang-") {
			return elixirBuild{tag: v, detail: "a full hexpm/elixir tag from build.version"}, nil
		}
		b, err := resolveElixirVersion(v, "")
		if err != nil {
			return b, fmt.Errorf("build.version: %w", err)
		}
		b.detail += " (from build.version)"
		return b, nil
	}

	if pin := s.pin; pin != nil {
		otp := otpMajor(pin.erlang)
		b, err := resolveElixirVersion(pin.elixir, otp)
		if err != nil && otp != "" {
			// The Elixir is fine but its OTP isn't one we build; a different OTP
			// within the same Elixir is the least surprising substitute.
			if fallback, ferr := resolveElixirVersion(pin.elixir, ""); ferr == nil {
				s.Event("config", "elixir-version", fmt.Sprintf("Elixir %s isn't built on OTP %s; building %s instead", pin.elixir, otp, fallback.detail))
				b, err = fallback, nil
			}
		}
		if err != nil {
			return b, fmt.Errorf("%s: %w", pin.source, err)
		}
		b.detail += " (from " + pin.source + ")"
		return b, nil
	}

	b, err := resolveElixirVersion(imagerefs.ElixirDefaultVersion, "")
	b.detail += " (default)"
	return b, err
}
