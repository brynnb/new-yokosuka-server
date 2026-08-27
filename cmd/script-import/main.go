package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/brynnb/new-yokosuka-server/internal/officialscript"
	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

func main() {
	inputPath := flag.String("input", "-", "JSON import batch path, or - for stdin")
	format := flag.String("format", "recovered-reference", "recovered-reference or official-yarn")
	builtin := flag.String("builtin", "", "import a reviewed built-in official translation")
	flag.Parse()
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	var reader io.Reader = os.Stdin
	if *inputPath != "-" {
		file, err := os.Open(*inputPath)
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()
		reader = file
	}
	database, err := store.Open(context.Background(), databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()
	if *builtin != "" {
		compilerPath := strings.TrimSpace(os.Getenv("YARN_COMPILER_PATH"))
		compiler, err := scriptcontent.NewProcessCompiler(compilerPath)
		if err != nil {
			log.Fatal(err)
		}
		database.SetScriptCompiler(compiler)
		names := []string{*builtin}
		if *builtin == "all" {
			names = officialscript.Names()
		}
		for _, name := range names {
			input, found := officialscript.Lookup(name)
			if !found {
				log.Fatalf("unknown built-in official script %q", name)
			}
			detail, err := database.ImportOfficialYarnScript(context.Background(), input)
			if err != nil {
				log.Fatalf("import built-in %s: %v", name, err)
			}
			fmt.Printf("%s\tv%d\t%s\n", detail.Slug, detail.CurrentVersion.Version, detail.CurrentVersion.Status)
		}
		return
	}
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	switch *format {
	case "recovered-reference":
		var inputs []store.RecoveredScriptImport
		if err := decoder.Decode(&inputs); err != nil {
			log.Fatalf("decode import batch: %v", err)
		}
		for _, input := range inputs {
			detail, err := database.ImportRecoveredScript(context.Background(), input)
			if err != nil {
				log.Fatalf("import %s: %v", input.SourceLocator, err)
			}
			fmt.Printf("%s\tv%d\t%s\n", detail.Slug, detail.CurrentVersion.Version, detail.CurrentVersion.Status)
		}
	case "official-yarn":
		compilerPath := strings.TrimSpace(os.Getenv("YARN_COMPILER_PATH"))
		compiler, err := scriptcontent.NewProcessCompiler(compilerPath)
		if err != nil {
			log.Fatal(err)
		}
		database.SetScriptCompiler(compiler)
		var inputs []store.OfficialYarnImport
		if err := decoder.Decode(&inputs); err != nil {
			log.Fatalf("decode import batch: %v", err)
		}
		for _, input := range inputs {
			detail, err := database.ImportOfficialYarnScript(context.Background(), input)
			if err != nil {
				log.Fatalf("import %s: %v", input.SourceLocator, err)
			}
			fmt.Printf("%s\tv%d\t%s\n", detail.Slug, detail.CurrentVersion.Version, detail.CurrentVersion.Status)
		}
	default:
		log.Fatalf("unsupported import format %q", *format)
	}
}
