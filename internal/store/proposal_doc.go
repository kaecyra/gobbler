package store

import (
	"encoding/json"
	"fmt"
)

// The document of an ingestion draft is owned by package ingest, which cannot
// be imported here. The store needs one thing of it: the catalog ingredient ids
// it names, to index them and to re-point them in an ingredient merge. That is
// the contract between the packages: a draft document is a JSON object whose
// "components" array holds objects whose "lines" array holds objects with a
// numeric "ingredient" (0 or absent when the line is unmatched). Every
// other field passes through with its value untouched (a re-point re-encodes
// the document, so its whitespace and key order may change).
// TestDraftDocumentIngredientsSurviveMerge in package ingest holds the other
// end of the contract.

type rawObject = map[string]json.RawMessage

func draftIngredientIDs(doc []byte) ([]int64, error) {
	var ids []int64
	seen := map[int64]bool{}
	err := walkDraftLines(doc, func(line rawObject) error {
		id, err := lineIngredient(line)
		if err != nil {
			return err
		}
		if id != 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
		return nil
	})
	return ids, err
}

func repointDraft(doc []byte, loser, winner int64) ([]byte, error) {
	var top rawObject
	if err := json.Unmarshal(doc, &top); err != nil {
		return nil, fmt.Errorf("decode draft document: %w", err)
	}
	var comps []rawObject
	if raw, ok := top["components"]; ok {
		if err := json.Unmarshal(raw, &comps); err != nil {
			return nil, fmt.Errorf("decode draft document components: %w", err)
		}
	}
	for _, c := range comps {
		var lines []rawObject
		raw, ok := c["lines"]
		if !ok {
			continue
		}
		if err := json.Unmarshal(raw, &lines); err != nil {
			return nil, fmt.Errorf("decode draft document lines: %w", err)
		}
		for _, l := range lines {
			id, err := lineIngredient(l)
			if err != nil {
				return nil, err
			}
			if id == loser {
				l["ingredient"] = json.RawMessage(fmt.Sprint(winner))
			}
		}
		b, err := json.Marshal(lines)
		if err != nil {
			return nil, fmt.Errorf("encode draft document lines: %w", err)
		}
		c["lines"] = b
	}
	if comps != nil {
		b, err := json.Marshal(comps)
		if err != nil {
			return nil, fmt.Errorf("encode draft document components: %w", err)
		}
		top["components"] = b
	}
	out, err := json.Marshal(top)
	if err != nil {
		return nil, fmt.Errorf("encode draft document: %w", err)
	}
	return out, nil
}

// walkDraftLines calls fn on every line object of a draft document.
func walkDraftLines(doc []byte, fn func(line rawObject) error) error {
	var top struct {
		Components []struct {
			Lines []rawObject `json:"lines"`
		} `json:"components"`
	}
	if err := json.Unmarshal(doc, &top); err != nil {
		return fmt.Errorf("decode draft document: %w", err)
	}
	for _, c := range top.Components {
		for _, l := range c.Lines {
			if err := fn(l); err != nil {
				return err
			}
		}
	}
	return nil
}

func lineIngredient(line rawObject) (int64, error) {
	raw, ok := line["ingredient"]
	if !ok {
		return 0, nil
	}
	var id int64
	if err := json.Unmarshal(raw, &id); err != nil {
		return 0, fmt.Errorf("draft document line ingredient %s: %w", raw, err)
	}
	return id, nil
}
