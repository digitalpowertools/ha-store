// Package sudoku generates and renders daily sudoku puzzles for the
// Little Printer. Puzzles are deterministic: the seed derives from the
// calendar date (plus difficulty), so the same day always prints the
// same puzzle — no storage needed.
package sudoku

import (
	"fmt"
	"math/rand"
	"time"
)

// Grid is a 9x9 sudoku in row-major order; 0 = empty.
type Grid [81]int

// Difficulty presets: number of givens. Fewer givens = harder.
type Difficulty struct {
	Name   string
	Givens int
}

var (
	Easy   = Difficulty{"easy", 40}
	Medium = Difficulty{"medium", 32}
	Hard   = Difficulty{"hard", 26}
)

// DifficultyByName maps a query parameter to a preset.
func DifficultyByName(name string) (Difficulty, error) {
	switch name {
	case "", "medium", "normal":
		return Medium, nil
	case "easy":
		return Easy, nil
	case "hard":
		return Hard, nil
	default:
		return Difficulty{}, fmt.Errorf("unknown difficulty %q (want easy, medium or hard)", name)
	}
}

// box returns the index of the 3x3 box for a cell.
func box(i int) int { return (i/27)*3 + (i%9)/3 }

// solver state: bitmask per row/col/box of used digits (bit d = digit d+1).
type board struct {
	cells          Grid
	rows, cols     [9]int
	boxes          [9]int
	empty          int
}

func (b *board) place(i, d int) {
	b.cells[i] = d
	bit := 1 << d
	b.rows[i/9] |= bit
	b.cols[i%9] |= bit
	b.boxes[box(i)] |= bit
	b.empty--
}

func (b *board) remove(i, d int) {
	b.cells[i] = 0
	bit := 1 << d
	b.rows[i/9] &^= bit
	b.cols[i%9] &^= bit
	b.boxes[box(i)] &^= bit
	b.empty++
}

// candidates returns the digits placeable at i, in 1..9 order.
func (b *board) candidates(i int) []int {
	used := b.rows[i/9] | b.cols[i%9] | b.boxes[box(i)]
	var out []int
	for d := 1; d <= 9; d++ {
		if used&(1<<d) == 0 {
			out = append(out, d)
		}
	}
	return out
}

// fill solves the board completely, choosing candidates in random
// order. Assumes an empty (or valid partial) board.
func (b *board) fill(rng *rand.Rand) bool {
	// Pick the most constrained empty cell (MRV keeps this fast).
	best, bestOpts := -1, []int(nil)
	for i := 0; i < 81; i++ {
		if b.cells[i] != 0 {
			continue
		}
		opts := b.candidates(i)
		if len(opts) == 0 {
			return false
		}
		if best == -1 || len(opts) < len(bestOpts) {
			best, bestOpts = i, opts
			if len(opts) == 1 {
				break
			}
		}
	}
	if best == -1 {
		return true // solved
	}

	// Randomize candidate order for unbiased generation.
	rng.Shuffle(len(bestOpts), func(a, b int) { bestOpts[a], bestOpts[b] = bestOpts[b], bestOpts[a] })
	for _, d := range bestOpts {
		b.place(best, d)
		if b.fill(rng) {
			return true
		}
		b.remove(best, d)
	}
	return false
}

// countSolutions counts solutions up to limit (used to test uniqueness).
func (b *board) countSolutions(limit int) int {
	if b.empty == 0 {
		return 1
	}
	best, bestOpts := -1, []int(nil)
	for i := 0; i < 81; i++ {
		if b.cells[i] != 0 {
			continue
		}
		opts := b.candidates(i)
		if len(opts) == 0 {
			return 0
		}
		if best == -1 || len(opts) < len(bestOpts) {
			best, bestOpts = i, opts
			if len(opts) == 1 {
				break
			}
		}
	}
	if best == -1 {
		return 1
	}
	n := 0
	for _, d := range bestOpts {
		b.place(best, d)
		n += b.countSolutions(limit - n)
		b.remove(best, d)
		if n >= limit {
			break
		}
	}
	return n
}

// Generate creates a puzzle with roughly the requested number of
// givens and a unique solution, seeded by seed.
func Generate(seed int64, givens int) (puzzle Grid, solution Grid) {
	rng := rand.New(rand.NewSource(seed))

	b := &board{empty: 81}
	if !b.fill(rng) {
		panic("sudoku: fill failed") // cannot happen on an empty board
	}
	solution = b.cells

	// Dig holes in random order, keeping the solution unique.
	order := make([]int, 81)
	for i := range order {
		order[i] = i
	}
	rng.Shuffle(len(order), func(a, b int) { order[a], order[b] = order[b], order[a] })

	removed := 0
	for _, i := range order {
		if removed >= 81-givens {
			break
		}
		d := b.cells[i]
		b.remove(i, d)
		if b.countSolutions(2) == 1 {
			removed++
		} else {
			b.place(i, d)
		}
	}
	return b.cells, solution
}

// PuzzleNumber returns days since 2026-01-01 for the given date
// (YYYY-MM-DD) — a stable, human-sensible "puzzle no." for printouts.
func PuzzleNumber(date string) int {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return 0
	}
	return int(t.Sub(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)).Hours() / 24)
}

// seedFor hashes date+salt into a stable int64 seed.
func seedFor(date, extra string) int64 {
	const (
		offset64 = -3750763034362895579 // FNV-1a offset basis (2^64 + offset)
		prime64  = 1099511628211
	)
	h := int64(offset64)
	for _, c := range date + "\x00" + extra {
		h ^= int64(c)
		h *= prime64
	}
	return h
}

// SeedFor returns the generation seed for a date and difficulty name.
func SeedFor(date, difficulty string) int64 {
	return seedFor(date, difficulty)
}
