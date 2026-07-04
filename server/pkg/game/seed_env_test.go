package game

import (
	"os"
	"testing"
)

// TestSeedFromEnvOrTime pins down the env-var path added in issue #47
// Fix A. The production path (env unset → time-based seed) is also
// exercised but we only assert that it returns a non-zero value (the
// actual number varies per call).
func TestSeedFromEnvOrTime(t *testing.T) {
	// Save and restore env so the test doesn't leak state.
	t.Setenv(testSeedEnvVar, "")
	defer func() {
		if err := os.Unsetenv(testSeedEnvVar); err != nil {
			t.Logf("Unsetenv cleanup: %v", err)
		}
	}()

	// Case 1: env unset → time-based seed (non-zero, varies).
	t.Run("env_unset_returns_time_based_seed", func(t *testing.T) {
		os.Unsetenv(testSeedEnvVar)
		s1 := seedFromEnvOrTime()
		s2 := seedFromEnvOrTime()
		if s1 == 0 {
			t.Fatal("seed should be non-zero when env unset")
		}
		// Two consecutive calls should almost always differ (the time
		// source has nanosecond resolution). Equal values would indicate
		// the function fell back to a constant — a regression.
		if s1 == s2 {
			t.Logf("note: two consecutive time-based seeds matched (%d) — "+
				"unlikely but not impossible within the same nanosecond", s1)
		}
	})

	// Case 2: valid int64 env → that exact seed.
	t.Run("env_set_returns_pinned_seed", func(t *testing.T) {
		os.Setenv(testSeedEnvVar, "42")
		if got := seedFromEnvOrTime(); got != 42 {
			t.Errorf("with PAPER_WAR_TEST_SEED=42, got seed %d, want 42", got)
		}
	})

	t.Run("env_set_to_large_int", func(t *testing.T) {
		os.Setenv(testSeedEnvVar, "9223372036854775807") // max int64
		if got := seedFromEnvOrTime(); got != 9223372036854775807 {
			t.Errorf("got seed %d, want max int64", got)
		}
	})

	t.Run("env_set_to_negative", func(t *testing.T) {
		os.Setenv(testSeedEnvVar, "-1")
		if got := seedFromEnvOrTime(); got != -1 {
			t.Errorf("got seed %d, want -1", got)
		}
	})

	// Case 3: invalid env → falls back to time-based (non-zero).
	t.Run("env_invalid_falls_back_to_time", func(t *testing.T) {
		os.Setenv(testSeedEnvVar, "not-a-number")
		if got := seedFromEnvOrTime(); got == 0 {
			t.Error("invalid env should fall back to non-zero time-based seed")
		}
	})

	t.Run("env_empty_falls_back_to_time", func(t *testing.T) {
		os.Setenv(testSeedEnvVar, "")
		if got := seedFromEnvOrTime(); got == 0 {
			t.Error("empty env should fall back to non-zero time-based seed")
		}
	})
}
