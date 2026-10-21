package unitseed

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/vocabulary"
)

// Graph is the unit graph of the seed alone, as a fresh repository holds it,
// and every problem the build names (none, for the shipped files). It is for
// readers outside a repository: the tests that hold the shipped samples to
// the shipped units.
func Graph() (*vocabulary.UnitGraph, []vocabulary.UnitProblem, error) {
	var us []vocabulary.UnitRecord
	var cs []vocabulary.ConversionRecord
	for _, file := range [][]byte{units, conversions} {
		dec := yaml.NewDecoder(bytes.NewReader(file))
		for {
			var doc struct {
				Kind     string `yaml:"kind"`
				Metadata struct {
					ID string `yaml:"id"`
				} `yaml:"metadata"`
				Data struct {
					Properties map[string]any `yaml:"properties"`
				} `yaml:"data"`
			}
			err := dec.Decode(&doc)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, nil, fmt.Errorf("unitseed: %w", err)
			}
			switch doc.Kind {
			case vocabulary.KindUnit:
				u, err := vocabulary.CheckUnitRecord(doc.Metadata.ID, doc.Data.Properties)
				if err != nil {
					return nil, nil, fmt.Errorf("unitseed: unit %s: %w", doc.Metadata.ID, err)
				}
				us = append(us, u)
			case vocabulary.KindConversion:
				c, err := vocabulary.CheckConversionRecord(doc.Metadata.ID, doc.Data.Properties)
				if err != nil {
					return nil, nil, fmt.Errorf("unitseed: conversion %s: %w", doc.Metadata.ID, err)
				}
				cs = append(cs, c)
			default:
				return nil, nil, fmt.Errorf("unitseed: a document of kind %q", doc.Kind)
			}
		}
	}
	g, problems := vocabulary.BuildUnitGraph(us, cs)
	return g, problems, nil
}
