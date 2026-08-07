package commands

import (
	"os"
	"slices"
	"sort"
	"strings"
)

// LocalEnvVar represents an environment variable found in the local environment
type LocalEnvVar struct {
	Key       string
	Value     string
	HasValue  bool   // true if the env var is set (even if empty)
	Source    string // "detected" (from stackbuild), "local" (found in env), "both"
	Sensitive bool   // heuristic: looks like a secret/key/token
}

// LocalEnvDetection contains the results of scanning the local environment
type LocalEnvDetection struct {
	// Available are env vars that are both detected and available locally
	Available []LocalEnvVar
	// Missing are env vars that were detected but not found locally
	Missing []LocalEnvVar
	// Additional are env vars found locally that look app-related but weren't detected
	Additional []LocalEnvVar
}

// commonEnvVarPrefixes are prefixes that often indicate app-related env vars
var commonEnvVarPrefixes = []string{
	"DATABASE_",
	"DB_",
	"REDIS_",
	"POSTGRES_",
	"MYSQL_",
	"MONGODB_",
	"MONGO_",
	"AWS_",
	"S3_",
	"STRIPE_",
	"TWILIO_",
	"SENDGRID_",
	"MAILGUN_",
	"SENTRY_",
	"HONEYBADGER_",
	"ROLLBAR_",
	"BUGSNAG_",
	"NEW_RELIC_",
	"DATADOG_",
	"ELASTICSEARCH_",
	"OPENAI_",
	"ANTHROPIC_",
	"GOOGLE_",
	"GITHUB_",
	"GITLAB_",
	"SLACK_",
	"DISCORD_",
	"PUSHER_",
	"CLOUDINARY_",
	"SMTP_",
	"MAIL_",
	"EMAIL_",
	"JWT_",
	"AUTH_",
	"OAUTH_",
	"SESSION_",
	"CACHE_",
	"MEMCACHE_",
}

// commonEnvVarSuffixes are suffixes that often indicate app-related env vars
var commonEnvVarSuffixes = []string{
	"_URL",
	"_URI",
	"_HOST",
	"_KEY",
	"_SECRET",
	"_TOKEN",
	"_PASSWORD",
	"_API_KEY",
	"_ACCESS_KEY",
	"_PRIVATE_KEY",
	"_DSN",
	"_ENDPOINT",
	"_CREDENTIALS",
}

// sensitivePatterns are substrings that indicate a value should be treated as sensitive
var sensitivePatterns = []string{
	"SECRET",
	"PASSWORD",
	"TOKEN",
	"KEY",
	"CREDENTIAL",
	"PRIVATE",
	"AUTH",
}

// credentialURLPrefixes name services whose connection strings (URL/URI)
// commonly embed credentials (e.g. postgres://user:pass@host/db). Keys
// matching one of these prefixes followed by _URL/_URI are masked so
// `miren deploy --analyze` doesn't print full DSNs to terminals or CI logs.
var credentialURLPrefixes = []string{
	"DATABASE",
	"DB",
	"REDIS",
	"POSTGRES",
	"POSTGRESQL",
	"MYSQL",
	"MARIADB",
	"MONGO",
	"MONGODB",
	"ELASTICSEARCH",
	"RABBITMQ",
	"AMQP",
	"CLOUDAMQP",
	"MEMCACHED",
	"MEMCACHIER",
	"CLOUDINARY",
}

// ignoredLocalEnvVars are env vars that should be ignored when scanning
var ignoredLocalEnvVars = map[string]bool{
	// System
	"PATH": true, "HOME": true, "USER": true, "SHELL": true,
	"LANG": true, "LC_ALL": true, "LC_CTYPE": true,
	"TERM": true, "TERM_PROGRAM": true, "TERM_SESSION_ID": true,
	"PWD": true, "OLDPWD": true, "SHLVL": true,
	"LOGNAME": true, "HOSTNAME": true, "HOSTTYPE": true,
	"OSTYPE": true, "MACHTYPE": true,
	"DISPLAY": true, "SSH_AUTH_SOCK": true, "SSH_AGENT_PID": true,
	"TMPDIR": true, "TEMP": true, "TMP": true,
	"XDG_CONFIG_HOME": true, "XDG_DATA_HOME": true, "XDG_CACHE_HOME": true,
	"XDG_RUNTIME_DIR": true, "XDG_SESSION_TYPE": true,

	// Editor/IDE
	"EDITOR": true, "VISUAL": true, "PAGER": true,
	"VSCODE_GIT_IPC_HANDLE": true, "VSCODE_GIT_ASKPASS_NODE": true,
	"VSCODE_GIT_ASKPASS_MAIN": true, "VSCODE_GIT_ASKPASS_EXTRA_ARGS": true,

	// Build/Dev tools
	"GOPATH": true, "GOROOT": true, "GOBIN": true,
	"CARGO_HOME": true, "RUSTUP_HOME": true,
	"NVM_DIR": true, "NVM_BIN": true, "NVM_INC": true,
	"RBENV_ROOT": true, "PYENV_ROOT": true,
	"VIRTUAL_ENV": true, "CONDA_DEFAULT_ENV": true,

	// CI/CD (usually set by CI systems, not by users)
	"CI": true, "CONTINUOUS_INTEGRATION": true,
	"BUILD_NUMBER": true, "BUILD_ID": true,
	"GITHUB_ACTIONS": true, "GITHUB_WORKFLOW": true,
	"GITLAB_CI": true, "CIRCLECI": true, "TRAVIS": true,

	// Ruby/Rails specific that are set by the runtime
	"RAILS_ENV": true, "RACK_ENV": true, "BUNDLE_PATH": true,
	"BUNDLE_WITHOUT": true, "GEM_HOME": true, "GEM_PATH": true,

	// Node specific
	"NODE_ENV": true, "NPM_CONFIG_PREFIX": true,

	// Python specific
	"PYTHONPATH": true, "PYTHONDONTWRITEBYTECODE": true,

	// General
	"PORT": true, "DEBUG": true,
	"TZ": true, "COLORTERM": true,

	// macOS specific
	"Apple_PubSub_Socket_Render": true, "__CF_USER_TEXT_ENCODING": true,
	"SECURITYSESSIONID": true, "LaunchInstanceID": true,
}

// DetectLocalEnvVars scans the local environment and cross-references with detected env vars
func DetectLocalEnvVars(detectedKeys []string) LocalEnvDetection {
	var result LocalEnvDetection

	// Build a set of detected keys for quick lookup
	detectedSet := make(map[string]bool)
	for _, key := range detectedKeys {
		detectedSet[key] = true
	}

	// Get all environment variables
	environ := os.Environ()
	localEnv := make(map[string]string)
	for _, e := range environ {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			localEnv[parts[0]] = parts[1]
		}
	}

	// Check which detected vars are available locally. The analyzer can
	// emit the same key from multiple sources (package-based and
	// source-based, for example), so dedupe before populating output rows.
	processed := make(map[string]bool)
	for _, key := range detectedKeys {
		if processed[key] {
			continue
		}
		processed[key] = true

		isSensitive := looksLikeSensitive(key)

		if value, exists := localEnv[key]; exists {
			result.Available = append(result.Available, LocalEnvVar{
				Key:       key,
				Value:     value,
				HasValue:  true,
				Source:    "both",
				Sensitive: isSensitive,
			})
		} else {
			result.Missing = append(result.Missing, LocalEnvVar{
				Key:       key,
				HasValue:  false,
				Source:    "detected",
				Sensitive: isSensitive,
			})
		}
	}

	// Find additional app-related env vars in local environment
	for key, value := range localEnv {
		// Skip if already in detected set
		if detectedSet[key] {
			continue
		}

		// Skip ignored vars
		if ignoredLocalEnvVars[key] {
			continue
		}

		// Check if it looks like an app-related env var
		if looksLikeAppEnvVar(key) {
			result.Additional = append(result.Additional, LocalEnvVar{
				Key:       key,
				Value:     value,
				HasValue:  true,
				Source:    "local",
				Sensitive: looksLikeSensitive(key),
			})
		}
	}

	// Sort results for consistent output
	sort.Slice(result.Available, func(i, j int) bool {
		return result.Available[i].Key < result.Available[j].Key
	})
	sort.Slice(result.Missing, func(i, j int) bool {
		return result.Missing[i].Key < result.Missing[j].Key
	})
	sort.Slice(result.Additional, func(i, j int) bool {
		return result.Additional[i].Key < result.Additional[j].Key
	})

	return result
}

// looksLikeAppEnvVar checks if an env var name looks like it's app-related
func looksLikeAppEnvVar(key string) bool {
	upperKey := strings.ToUpper(key)

	// Check prefixes
	for _, prefix := range commonEnvVarPrefixes {
		if strings.HasPrefix(upperKey, prefix) {
			return true
		}
	}

	// Check suffixes
	for _, suffix := range commonEnvVarSuffixes {
		if strings.HasSuffix(upperKey, suffix) {
			return true
		}
	}

	// Check for common standalone names
	standaloneNames := []string{
		"SECRET_KEY_BASE",
		"ENCRYPTION_KEY",
		"MASTER_KEY",
		"APPLICATION_SECRET",
		"APP_SECRET",
	}
	return slices.Contains(standaloneNames, upperKey)
}

// looksLikeSensitive guesses, from a key NAME alone, whether a var is likely a
// secret. This is a tier-3 heuristic (a guess), and its only sanctioned use is
// as a set-time *suggestion* the user can override — e.g. proposing a default
// `sensitive` flag for locally-detected vars during deploy analysis. It must
// never be the sole authority for masking at display time: that is what
// maskEnvValue (flag + structural URL invariant) is for, and it backstops any
// miss here. See the maskEnvValue doc comment for the full evidence-tier rule.
func looksLikeSensitive(key string) bool {
	upperKey := strings.ToUpper(key)
	for _, pattern := range sensitivePatterns {
		if strings.Contains(upperKey, pattern) {
			return true
		}
	}
	// DSNs (e.g. SENTRY_DSN) embed credentials regardless of prefix.
	if strings.HasSuffix(upperKey, "_DSN") {
		return true
	}
	// Connection-string URLs/URIs for known credential-bearing services.
	if strings.HasSuffix(upperKey, "_URL") || strings.HasSuffix(upperKey, "_URI") {
		for _, prefix := range credentialURLPrefixes {
			if strings.HasPrefix(upperKey, prefix+"_") {
				return true
			}
		}
	}
	return false
}
