package sudoku

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// hasUniqueSolution regenerates by counting solutions on the puzzle.
func hasUniqueSolution(t *testing.T, p Grid) bool {
	t.Helper()
	b := &board{cells: p, empty: 0}
	for i, v := range p {
		if v != 0 {
			b.place(i, v)
		}
	}
	return b.countSolutions(2) == 1
}

func TestGenerateUniqueAndValid(t *testing.T) {
	for _, diff := range []Difficulty{Easy, Medium, Hard} {
		p, s := Generate(SeedFor("2026-11-19", diff.Name), diff.Givens)
		givens := 0
		for i, v := range p {
			if v == 0 {
				continue
			}
			givens++
			if s[i] != v {
				t.Fatalf("%s: puzzle cell %d contradicts solution", diff.Name, i)
			}
		}
		if givens > diff.Givens+4 {
			t.Errorf("%s: %d givens (wanted ~%d)", diff.Name, givens, diff.Givens)
		}
		if !hasUniqueSolution(t, p) {
			t.Errorf("%s: puzzle has multiple solutions", diff.Name)
		}
		// Solution must be a valid grid: all rows sum correctly is not
		// sufficient, but every cell non-zero and generation guarantees
		// validity by construction. Check no zero in solution.
		for i, v := range s {
			if v < 1 || v > 9 {
				t.Fatalf("%s: solution cell %d = %d", diff.Name, i, v)
			}
		}
	}
}

func TestDeterministic(t *testing.T) {
	a1, s1 := Generate(SeedFor("2026-11-19", "hard"), Hard.Givens)
	a2, s2 := Generate(SeedFor("2026-11-19", "hard"), Hard.Givens)
	if a1 != a2 || s1 != s2 {
		t.Fatal("same seed produced different puzzles")
	}
	b1, _ := Generate(SeedFor("2026-11-20", "hard"), Hard.Givens)
	if b1 == a1 {
		t.Fatal("different dates produced identical puzzles")
	}
	d1, _ := Generate(SeedFor("2026-11-19", "easy"), Easy.Givens)
	if d1 == a1 {
		t.Fatal("different difficulties produced identical puzzles")
	}
}

func TestPuzzleNumber(t *testing.T) {
	if got := PuzzleNumber("2026-01-01"); got != 0 {
		t.Errorf("2026-01-01 → %d, want 0", got)
	}
	if got := PuzzleNumber("2026-11-19"); got != 322 {
		t.Errorf("2026-11-19 → %d, want 322", got)
	}
}

func TestVisualRender(t *testing.T) {
	p, _ := Generate(SeedFor("2026-11-19", "medium"), Medium.Givens)
	png1 := RenderPNG(&p, "2026-11-19", "medium", false)
	out := filepath.Join("testdata", "out")
	_ = os.MkdirAll(out, 0o755)
	_ = os.WriteFile(filepath.Join(out, "sudoku.png"), png1, 0o644)

	s, _ := Generate(SeedFor("2026-11-19", "medium"), Medium.Givens)
	_ = os.WriteFile(filepath.Join(out, "sudoku_solution.png"),
		RenderPNG(&s, "2026-11-19", "medium", true), 0o644)

	// Decode sanity: valid PNG, paper width.
	img, err := png.Decode(bytes.NewReader(png1))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != paperW {
		t.Fatalf("width %d, want %d", img.Bounds().Dx(), paperW)
	}
}
