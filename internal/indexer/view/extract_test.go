package view_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/akyrey/koseven-lsp/internal/indexer/view"
	"github.com/akyrey/koseven-lsp/internal/phpparse"
)

// extractUsages is a helper that parses src and returns the first ViewUsage
// for viewName, along with its ExposedVars keyed by name.
func extractUsages(t *testing.T, src []byte) []view.ViewUsage {
	t.Helper()
	root, err := phpparse.Bytes(src, "test.php")
	require.NoError(t, err)
	usages, _ := view.ExtractFileUsages("test.php", root, src)
	return usages
}

func varsByName(usages []view.ViewUsage, viewName string) map[string]view.ExposedVar {
	out := make(map[string]view.ExposedVar)
	for _, u := range usages {
		if u.Name == viewName {
			for _, v := range u.ExposedVars {
				out[v.Name] = v
			}
		}
	}
	return out
}

// ─── Single-hop (regression) ─────────────────────────────────────────────────

func TestExtract_SplitAssignment_SingleHop(t *testing.T) {
	src := []byte(`<?php
$view = View::factory('pages/test');
$view->set('alpha', 'a');
$view->bind('beta', $ref);
`)
	usages := extractUsages(t, src)
	vars := varsByName(usages, "pages/test")
	assert.Contains(t, vars, "alpha")
	assert.Contains(t, vars, "beta")
}

// ─── Two-hop chain ───────────────────────────────────────────────────────────

func TestExtract_SplitAssignment_TwoHopChain(t *testing.T) {
	// $view->set('first', ...)->set('second', ...) — both must be captured.
	src := []byte(`<?php
$view = View::factory('pages/test');
$view->set('first', 'a')->set('second', 'b');
`)
	usages := extractUsages(t, src)
	vars := varsByName(usages, "pages/test")
	assert.Contains(t, vars, "first", "direct receiver must still be captured")
	assert.Contains(t, vars, "second", "chained receiver must now be captured")
}

// ─── Three-hop chain ─────────────────────────────────────────────────────────

func TestExtract_SplitAssignment_ThreeHopChain(t *testing.T) {
	src := []byte(`<?php
$view = View::factory('pages/test');
$view->set('a', 1)->set('b', 2)->bind('c', $ref);
`)
	usages := extractUsages(t, src)
	vars := varsByName(usages, "pages/test")
	assert.Contains(t, vars, "a")
	assert.Contains(t, vars, "b")
	assert.Contains(t, vars, "c")
}

// ─── Mixed: separate + chained calls ─────────────────────────────────────────

func TestExtract_SplitAssignment_MixedSeparateAndChained(t *testing.T) {
	src := []byte(`<?php
$view = View::factory('pages/test');
$view->set('standalone', 'x');
$view->set('chain_a', 1)->set('chain_b', 2);
`)
	usages := extractUsages(t, src)
	vars := varsByName(usages, "pages/test")
	assert.Contains(t, vars, "standalone")
	assert.Contains(t, vars, "chain_a")
	assert.Contains(t, vars, "chain_b")
}

// ─── No cross-view contamination ─────────────────────────────────────────────

func TestExtract_SplitAssignment_ChainDoesNotPolluteSiblingView(t *testing.T) {
	src := []byte(`<?php
$a = View::factory('pages/alpha');
$b = View::factory('pages/beta');
$a->set('x', 1)->set('y', 2);
$b->set('z', 3);
`)
	usages := extractUsages(t, src)
	alphaVars := varsByName(usages, "pages/alpha")
	betaVars := varsByName(usages, "pages/beta")

	assert.Contains(t, alphaVars, "x")
	assert.Contains(t, alphaVars, "y")
	assert.NotContains(t, alphaVars, "z", "beta's var must not leak into alpha")

	assert.Contains(t, betaVars, "z")
	assert.NotContains(t, betaVars, "x", "alpha's chained var must not leak into beta")
	assert.NotContains(t, betaVars, "y")
}
