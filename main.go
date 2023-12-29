package main

import (
	"bufio"
	"errors"
	"flag"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/v2fly/v2ray-core/v5/app/router/routercommon"
)

var (
	dataPath     = flag.String("datapath", "./data", "Path to your custom 'data' directory")
	outputDir    = flag.String("outputdir", "./generated", "Directory to place all generated files")
	outputFormat = flag.String("outputformat", "surge", "Output type, support surge and quantumult")
)

type Entry struct {
	Type  string
	Value string
	Attrs []*routercommon.Domain_Attribute
}

type List struct {
	Name  string
	Entry []Entry
}

type ParsedList struct {
	Name      string
	Inclusion map[string]bool
	Entry     []Entry
}

type SurgeRuleSets map[string][]string

func (l *ParsedList) toSurge() (SurgeRuleSets, error) {
	ruleSets := make(SurgeRuleSets)
	for _, entry := range l.Entry {
		var rule string
		switch entry.Type {
		case "domain":
			rule = "DOMAIN-SUFFIX," + entry.Value
		case "regexp":
			log.Printf("Surge is not support regexp: %s in %s\n", entry.Value, l.Name)
			continue
		case "keyword":
			rule = "DOMAIN-KEYWORD," + entry.Value
		case "full":
			rule = "DOMAIN," + entry.Value
		default:
			return nil, errors.New("unknown domain type: " + entry.Type)
		}
		ruleSets.Add(l.Name, rule, entry.Attrs)
	}
	return ruleSets, nil
}

func (r *SurgeRuleSets) Add(code, rule string, attrs []*routercommon.Domain_Attribute) {
	(*r)[code] = append((*r)[code], rule)
	for _, attr := range attrs {
		(*r)[code+"@"+attr.Key] = append((*r)[code+"@"+attr.Key], rule)
	}
}

type QuantumultFilters map[string][]string

func (l *ParsedList) toQuantumult() (QuantumultFilters, error) {
	ruleSets := make(QuantumultFilters)
	for _, entry := range l.Entry {
		var rule string
		switch entry.Type {
		case "domain":
			rule = "HOST-SUFFIX," + entry.Value
		case "regexp":
			log.Printf("Quantumult is not support regexp: %s in %s\n", entry.Value, l.Name)
			continue
		case "keyword":
			rule = "HOST-KEYWORD," + entry.Value
		case "full":
			rule = "HOST," + entry.Value
		default:
			return nil, errors.New("unknown domain type: " + entry.Type)
		}
		ruleSets.Add(l.Name, rule, entry.Attrs)
	}
	return ruleSets, nil
}

func (r *QuantumultFilters) Add(code, rule string, attrs []*routercommon.Domain_Attribute) {
	(*r)[code] = append((*r)[code], rule+","+code)
	for _, attr := range attrs {
		(*r)[code+"@"+attr.Key] = append((*r)[code+"@"+attr.Key], rule+","+code+"@"+attr.Key)
	}
}

func removeComment(line string) string {
	idx := strings.Index(line, "#")
	if idx == -1 {
		return line
	}
	return strings.TrimSpace(line[:idx])
}

func parseDomain(domain string, entry *Entry) error {
	kv := strings.Split(domain, ":")
	if len(kv) == 1 {
		entry.Type = "domain"
		entry.Value = strings.ToLower(kv[0])
		return nil
	}

	if len(kv) == 2 {
		entry.Type = strings.ToLower(kv[0])
		entry.Value = strings.ToLower(kv[1])
		return nil
	}

	return errors.New("Invalid format: " + domain)
}

func parseAttribute(attr string) (*routercommon.Domain_Attribute, error) {
	var attribute routercommon.Domain_Attribute
	if len(attr) == 0 || attr[0] != '@' {
		return &attribute, errors.New("invalid attribute: " + attr)
	}

	// Trim attribute prefix `@` character
	attr = attr[1:]
	parts := strings.Split(attr, "=")
	if len(parts) == 1 {
		attribute.Key = strings.ToLower(parts[0])
		attribute.TypedValue = &routercommon.Domain_Attribute_BoolValue{BoolValue: true}
	} else {
		attribute.Key = strings.ToLower(parts[0])
		intv, err := strconv.Atoi(parts[1])
		if err != nil {
			return &attribute, errors.New("invalid attribute: " + attr + ": " + err.Error())
		}
		attribute.TypedValue = &routercommon.Domain_Attribute_IntValue{IntValue: int64(intv)}
	}
	return &attribute, nil
}

func parseEntry(line string) (Entry, error) {
	line = strings.TrimSpace(line)
	parts := strings.Split(line, " ")

	var entry Entry
	if len(parts) == 0 {
		return entry, errors.New("empty entry")
	}

	if err := parseDomain(parts[0], &entry); err != nil {
		return entry, err
	}

	for i := 1; i < len(parts); i++ {
		attr, err := parseAttribute(parts[i])
		if err != nil {
			return entry, err
		}
		entry.Attrs = append(entry.Attrs, attr)
	}

	return entry, nil
}

func Load(path string) (*List, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	list := &List{
		Name: strings.ToLower(filepath.Base(path)),
	}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		line = removeComment(line)
		if len(line) == 0 {
			continue
		}
		entry, err := parseEntry(line)
		if err != nil {
			return nil, err
		}
		list.Entry = append(list.Entry, entry)
	}

	return list, nil
}

func isMatchAttr(Attrs []*routercommon.Domain_Attribute, includeKey string) bool {
	isMatch := false
	mustMatch := true
	matchName := includeKey
	if strings.HasPrefix(includeKey, "!") {
		isMatch = true
		mustMatch = false
		matchName = strings.TrimLeft(includeKey, "!")
	}

	for _, Attr := range Attrs {
		attrName := Attr.Key
		if mustMatch {
			if matchName == attrName {
				isMatch = true
				break
			}
		} else {
			if matchName == attrName {
				isMatch = false
				break
			}
		}
	}
	return isMatch
}

func createIncludeAttrEntrys(list *List, matchAttr *routercommon.Domain_Attribute) []Entry {
	newEntryList := make([]Entry, 0, len(list.Entry))
	matchName := matchAttr.Key
	for _, entry := range list.Entry {
		matched := isMatchAttr(entry.Attrs, matchName)
		if matched {
			newEntryList = append(newEntryList, entry)
		}
	}
	return newEntryList
}

func ParseList(list *List, ref map[string]*List) (*ParsedList, error) {
	pl := &ParsedList{
		Name:      list.Name,
		Inclusion: make(map[string]bool),
	}
	entryList := list.Entry
	for {
		newEntryList := make([]Entry, 0, len(entryList))
		hasInclude := false
		for _, entry := range entryList {
			if entry.Type == "include" {
				refName := strings.ToLower(entry.Value)
				if entry.Attrs != nil {
					for _, attr := range entry.Attrs {
						InclusionName := strings.ToLower(refName + "@" + attr.Key)
						if pl.Inclusion[InclusionName] {
							continue
						}
						pl.Inclusion[InclusionName] = true

						refList := ref[refName]
						if refList == nil {
							return nil, errors.New(entry.Value + " not found.")
						}
						attrEntrys := createIncludeAttrEntrys(refList, attr)
						if len(attrEntrys) != 0 {
							newEntryList = append(newEntryList, attrEntrys...)
						}
					}
				} else {
					InclusionName := refName
					if pl.Inclusion[InclusionName] {
						continue
					}
					pl.Inclusion[InclusionName] = true
					refList := ref[refName]
					if refList == nil {
						return nil, errors.New(entry.Value + " not found.")
					}
					newEntryList = append(newEntryList, refList.Entry...)
				}
				hasInclude = true
			} else {
				newEntryList = append(newEntryList, entry)
			}
		}
		entryList = newEntryList
		if !hasInclude {
			break
		}
	}
	pl.Entry = entryList

	return pl, nil
}

func main() {
	flag.Parse()

	dir := *dataPath
	log.Println("Use domain lists in", dir)

	ref := make(map[string]*List)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		list, err := Load(path)
		if err != nil {
			return err
		}
		ref[list.Name] = list
		return nil
	})
	if err != nil {
		log.Println("Failed: ", err)
		os.Exit(1)
	}

	// Create output directory if not exist
	if _, err := os.Stat(*outputDir); os.IsNotExist(err) {
		if mkErr := os.MkdirAll(*outputDir, 0755); mkErr != nil {
			log.Println("Failed: ", mkErr)
			os.Exit(1)
		}
	}

	for _, list := range ref {
		pl, err := ParseList(list, ref)
		if err != nil {
			log.Println("Failed: ", err)
			os.Exit(1)
		}

		var ruleSets map[string][]string

		switch strings.ToLower(*outputFormat) {
		case "surge":
			ruleSets, err = pl.toSurge()
		case "quantumult", "quantumultx":
			ruleSets, err = pl.toQuantumult()
		default:
			log.Println("Unknown output type: ", *outputFormat)
			os.Exit(1)
		}

		if err != nil {
			log.Println("Failed: ", err)
			os.Exit(1)
		}

		for code, rules := range ruleSets {
			rulesContent := strings.Join(rules, "\n")
			if err := os.WriteFile(filepath.Join(*outputDir, code+".list"), []byte(rulesContent), 0644); err != nil {
				log.Println("Failed: ", err)
				os.Exit(1)
			}
		}
	}
}
