package main

// location.go — customize this file for your InvenTree location hierarchy.
//
// This file controls two things:
//   1. How stock item locations are displayed throughout the TUI (LocationPath).
//   2. How the "bin:" and "loc:" search filters match location names (matchBinFilter).
//
// The defaults assume a top-level "Workshop/" location and bins named "Bin 57",
// "Bin 3", etc.  Edit to match your own hierarchy and naming convention.

import "strings"

// LocationPath returns the display path for a stock item's bin.
// Returns "(no location)" when no location is assigned.
//
// To customize: change the CutPrefix argument to your top-level location name,
// or remove the CutPrefix block entirely to always show full paths.
func (s StockItem) LocationPath() string {
	if s.LocationDetail == nil || s.LocationDetail.PathString == "" {
		return "(no location)"
	}
	p := s.LocationDetail.PathString
	// Strip leading "Workshop/" so "Workshop/Bin 57" displays as "Bin 57".
	// Change "Workshop/" to your own top-level location name, or remove
	// these lines to show full paths everywhere.
	if after, ok := strings.CutPrefix(p, "Workshop/"); ok {
		return after
	}
	return p
}

// matchBinFilter reports whether the location represented by locNames/locPaths
// matches the "bin:VAL" or "loc:VAL" filter typed in the search box.
//
//   - locNames: lower-cased location names for this stock item (e.g. "bin 57")
//   - locPaths: lower-cased full path strings (e.g. "workshop/bin 57")
//   - val: the filter value, already lower-cased (e.g. "57" or "closet")
//
// To customize: adjust the name-matching logic to fit your bin naming convention.
// The default matches "bin N" when VAL is all digits ("bin:57" → "Bin 57"),
// and does a path substring match for any non-numeric VAL ("bin:closet").
func matchBinFilter(locNames, locPaths []string, val string) bool {
	if isDigits(val) {
		// "bin:57" matches a location whose name is exactly "Bin 57"
		for _, n := range locNames {
			if n == "bin "+val {
				return true
			}
		}
		return false
	}
	for _, p := range locPaths {
		if strings.Contains(p, val) {
			return true
		}
	}
	return false
}
