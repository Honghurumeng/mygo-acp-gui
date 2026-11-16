package ui

import (
	"path"
	"strings"

	mui "github.com/egoist/mygo/ui"
)

// File type icons are picked from the name alone: the tree never opens or
// stats a file to decide how to draw its row, so a wrong extension costs
// nothing and a huge directory stays cheap.
//
// The glyphs stay full-box (braces, angle brackets, a picture frame) instead
// of a page outline with a tiny mark inside: at the tree's 13 DIP font a page
// hides the mark, while these silhouettes are still distinct.
var fileIconGroups = []struct {
	icon *mui.SVG
	exts []string
}{
	{textIcon, []string{
		"md", "mdx", "markdown", "txt", "text", "rst", "adoc", "asciidoc",
		"log", "org", "tex", "csv", "tsv",
	}},
	{codeIcon, []string{
		"go", "rs", "py", "pyi", "pyw", "js", "mjs", "cjs", "jsx", "ts",
		"mts", "cts", "tsx", "c", "h", "cc", "cpp", "cxx", "hpp", "hxx",
		"hh", "java", "kt", "kts", "scala", "sc", "rb", "php", "swift", "m",
		"mm", "cs", "fs", "fsx", "lua", "zig", "pl", "pm", "r", "dart",
		"ex", "exs", "erl", "hrl", "hs", "lhs", "clj", "cljs", "cljc",
		"vue", "svelte", "astro", "sol", "nim", "jl", "groovy", "proto",
		"graphql", "gql", "sql", "asm", "s", "f90", "f95", "for", "pas",
		"elm", "ml", "mli", "rkt", "scm", "vim", "v", "ipynb", "html",
		"htm", "xhtml", "xml", "xsl", "xsd", "css", "scss", "sass", "less",
		"styl",
	}},
	{bracesIcon, []string{
		"json", "jsonc", "json5", "geojson", "webmanifest",
	}},
	{imageIcon, []string{
		"png", "jpg", "jpeg", "gif", "webp", "avif", "bmp", "ico", "icns",
		"tif", "tiff", "heic", "heif", "psd", "ai", "sketch", "svg", "eps",
	}},
	{terminalIcon, []string{
		"sh", "bash", "zsh", "fish", "ksh", "csh", "tcsh", "ps1", "psm1",
		"bat", "cmd",
	}},
	// Config files and build scripts: dotfiles, manifests and YAML/TOML all
	// read as settings, so they share the settings gear.
	{settingsIcon, []string{
		"yaml", "yml", "toml", "ini", "cfg", "conf", "config", "properties",
		"env", "plist", "mod", "sum", "lock", "mk", "cmake", "gradle",
		"tf", "tfvars", "hcl", "nix", "service", "desktop",
	}},
}

var fileIconByExt = func() map[string]*mui.SVG {
	icons := make(map[string]*mui.SVG)
	for _, group := range fileIconGroups {
		for _, ext := range group.exts {
			icons[ext] = group.icon
		}
	}
	return icons
}()

// fileIconByName covers the types that carry no extension at all.
var fileIconByName = func() map[string]*mui.SVG {
	icons := make(map[string]*mui.SVG)
	for _, name := range []string{
		"readme", "license", "licence", "copying", "notice", "changelog",
		"authors", "contributors", "codeowners", "todo",
	} {
		icons[name] = textIcon
	}
	for _, name := range []string{
		"makefile", "gnumakefile", "cmakelists.txt", "justfile", "rakefile",
		"gemfile", "podfile", "brewfile", "procfile", "dockerfile",
		"containerfile", "vagrantfile", "jenkinsfile", ".gitignore",
		".gitattributes", ".gitmodules", ".editorconfig", ".npmrc",
		".nvmrc", ".yarnrc", ".prettierrc", ".eslintrc", ".babelrc",
		".dockerignore", ".npmignore", ".tool-versions",
	} {
		icons[name] = settingsIcon
	}
	return icons
}()

// fileIconFor is the glyph a file row shows. The name is matched lowercased;
// ".env.local" and friends count as env files.
func fileIconFor(name string) *mui.SVG {
	base := strings.ToLower(path.Base(name))
	if icon, ok := fileIconByName[base]; ok {
		return icon
	}
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return settingsIcon
	}
	if icon, ok := fileIconByExt[strings.TrimPrefix(path.Ext(base), ".")]; ok {
		return icon
	}
	return fileIcon
}
