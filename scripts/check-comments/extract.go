package main

import (
	"bytes"
	"fmt"
	"runtime"
	"strings"
	"sync"
)

type env struct {
	root       string
	typescript string
	shfmt      string
}

func (e *env) extractAll(sources []source, withUnits bool) ([]fileResult, error) {
	results := make([]fileResult, len(sources))
	var scripts []int
	jobs := make(chan int)
	var wait sync.WaitGroup
	for worker := 0; worker < runtime.NumCPU(); worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for i := range jobs {
				results[i] = e.extractOne(sources[i], withUnits)
			}
		}()
	}
	for i, item := range sources {
		lang, ok := classify(item.rel)
		switch {
		case !ok:
			results[i] = fileResult{rel: item.rel, content: item.content, err: fmt.Errorf("unknown file type")}
		case bytes.IndexByte(item.content, 0) >= 0:
			results[i] = fileResult{rel: item.rel, lang: lang, content: item.content, binary: true}
		case lang == langTS:
			results[i] = fileResult{rel: item.rel, lang: lang, content: item.content}
			scripts = append(scripts, i)
		default:
			jobs <- i
		}
	}
	close(jobs)
	wait.Wait()
	if len(scripts) == 0 {
		return results, nil
	}
	if err := e.extractScripts(results, scripts, withUnits); err != nil {
		return nil, err
	}
	return results, nil
}

func (e *env) extractOne(item source, withUnits bool) fileResult {
	lang, _ := classify(item.rel)
	result := fileResult{rel: item.rel, lang: lang, content: item.content}
	switch lang {
	case langGo:
		result.comments, result.units = extractGo(item.content)
	case langRust:
		result.comments, result.units = extractRust(item.content)
	case langShell:
		result.comments, result.units, result.err = e.extractShell(item.rel, item.content, withUnits)
	case langYAML, langWorkflow, langHelm, langTemplate:
		result.comments, result.dataLines, result.err = e.extractYAML(lang, item.content)
	default:
		result.comments, result.dataLines = extractLineLanguage(lang, item.content)
	}
	result.comments = sortComments(result.comments)
	if usesLineUnits(lang) {
		result.units = lineUnits(result)
	}
	return result
}

func usesLineUnits(lang string) bool {
	switch lang {
	case langGo, langRust, langTS, langShell:
		return false
	}
	return true
}

func lineUnits(result fileResult) []string {
	var units []string
	for i, line := range strings.Split(codeText(result), "\n") {
		line = strings.TrimRight(line, " \t\r")
		if line == "" && !result.dataLines[i+1] {
			continue
		}
		units = append(units, line)
	}
	return units
}
