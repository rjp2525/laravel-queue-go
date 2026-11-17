package queue

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// LaravelVersion is the major version of the Laravel application this
// package shares a queue with. It decides the payload keys a Dispatcher
// writes, the default Redis key prefix and, on Redis Cluster, the key layout.
//
// The zero value is LaravelAuto.
type LaravelVersion int

const (
	// LaravelAuto detects the version from jobs already on the queue and
	// falls back to LatestLaravel when there is nothing to look at.
	LaravelAuto LaravelVersion = 0

	Laravel10 LaravelVersion = 10
	Laravel11 LaravelVersion = 11
	Laravel12 LaravelVersion = 12
	Laravel13 LaravelVersion = 13

	// LatestLaravel is the newest version this package knows. Its payload is
	// a superset of the older ones, which Laravel 10-12 read without trouble.
	LatestLaravel = Laravel13
)

func (v LaravelVersion) String() string {
	if v == LaravelAuto {
		return "auto"
	}
	return strconv.Itoa(int(v))
}

// Valid reports whether v is LaravelAuto or a supported major version.
func (v LaravelVersion) Valid() bool {
	return v == LaravelAuto || (v >= Laravel10 && v <= LatestLaravel)
}

// orLatest resolves LaravelAuto to LatestLaravel.
func (v LaravelVersion) orLatest() LaravelVersion {
	if v == LaravelAuto {
		return LatestLaravel
	}
	return v
}

// ParseLaravelVersion accepts "auto" (or ""), a major version such as "12",
// and the forms Composer prints: "12.x", "v12.3.1", "^12.0".
func ParseLaravelVersion(s string) (LaravelVersion, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == "auto" {
		return LaravelAuto, nil
	}
	major, _, _ := strings.Cut(strings.TrimLeft(s, "v^~>= "), ".")
	n, err := strconv.Atoi(major)
	if err != nil {
		return LaravelAuto, fmt.Errorf("invalid Laravel version %q", s)
	}
	v := LaravelVersion(n)
	if !v.Valid() || v == LaravelAuto {
		return LaravelAuto, fmt.Errorf("unsupported Laravel version %q (supported: %d-%d)", s, Laravel10, LatestLaravel)
	}
	return v, nil
}

// RedisPrefix returns the Redis key prefix a fresh Laravel application of
// this version uses for the given APP_NAME: config/database.php ships with
// Str::slug(APP_NAME, '_').'_database_' in Laravel 10 and 11, and
// Str::slug(APP_NAME).'-database-' from Laravel 12.
//
// The prefix lives in the application's own config file. An app created on
// Laravel 10 and upgraded since still has the underscore form, and REDIS_PREFIX
// overrides both. When in doubt, read it from the app:
//
//	php artisan tinker --execute="echo config('database.redis.options.prefix');"
func (v LaravelVersion) RedisPrefix(appName string) string {
	sep := "-"
	if v == Laravel10 || v == Laravel11 {
		sep = "_"
	}
	if appName == "" {
		appName = "laravel"
	}
	return slug(appName, sep) + sep + "database" + sep
}

// slug is Str::slug for ASCII input: lower-case, runs of anything other than
// letters and digits collapsed into sep. It does not transliterate.
func slug(s, sep string) string {
	var b strings.Builder
	pending := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if pending && b.Len() > 0 {
				b.WriteString(sep)
			}
			pending = false
			b.WriteRune(r)
			continue
		}
		pending = true
	}
	return b.String()
}

// DetectLaravelVersion infers the Laravel major version that produced a job
// payload from the keys it carries. ok is false when data is not a job
// payload.
//
// The answer is the oldest version consistent with the payload:
//
//   - countCrashesAsExceptions or deleteWhenMissingModels: Laravel 13
//   - createdAt, delay or data.batchId: Laravel 12
//   - illuminate:log:context: Laravel 11
//   - none of these: Laravel 10
//
// Laravel 11 only writes its marker when the app has put something in the
// Context facade, so an 11 payload is often reported as 10. The two versions
// use the same payload and prefix, which makes the difference harmless here.
func DetectLaravelVersion(data []byte) (v LaravelVersion, ok bool) {
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		return LaravelAuto, false
	}
	if _, isJob := keys["job"]; !isJob {
		return LaravelAuto, false
	}

	has := func(m map[string]json.RawMessage, names ...string) bool {
		for _, name := range names {
			if _, found := m[name]; found {
				return true
			}
		}
		return false
	}

	var dataKeys map[string]json.RawMessage
	_ = json.Unmarshal(keys["data"], &dataKeys) // "data" is not always an object

	switch {
	case has(keys, "countCrashesAsExceptions", "deleteWhenMissingModels"):
		return Laravel13, true
	case has(keys, "createdAt", "delay"), has(dataKeys, "batchId"):
		return Laravel12, true
	case has(keys, "illuminate:log:context"):
		return Laravel11, true
	}
	return Laravel10, true
}
