package introspect

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// TypeScriptIntrospector extracts tool metadata from TypeScript and JavaScript
// source files by running a Node.js subprocess with a regex-based parser.
type TypeScriptIntrospector struct{}

// tsExtractToolsScript is the Node.js script that parses exported functions
// from a TypeScript or JavaScript file using regex patterns and returns
// a JSON array of tool metadata. It is written to a temp file and executed
// via "node <script> <filepath>" to avoid escaping issues with -e.
const tsExtractToolsScript = `'use strict';
const fs = require('fs');
const path = require('path');

const filePath = process.argv[2];
const source = fs.readFileSync(filePath, 'utf-8');
const ext = path.extname(filePath);
const language = (ext === '.ts' || ext === '.mts') ? 'typescript' : 'javascript';

const typeMap = {
  'number': 'number',
  'string': 'string',
  'boolean': 'boolean',
  'object': 'object',
  'any': 'string',
  'void': 'string',
  'undefined': 'string',
  'null': 'string',
};

function mapType(tsType) {
  if (!tsType) return 'string';
  const t = tsType.trim();
  if (t.startsWith('Array') || t.endsWith('[]')) return 'array';
  return typeMap[t] || 'string';
}

function parseParams(paramStr) {
  if (!paramStr || !paramStr.trim()) return { properties: {}, required: [] };
  const properties = {};
  const required = [];
  let depth = 0;
  let current = '';
  const parts = [];
  for (let i = 0; i < paramStr.length; i++) {
    const ch = paramStr[i];
    if (ch === '<' || ch === '(' || ch === '{') depth++;
    else if (ch === '>' || ch === ')' || ch === '}') depth--;
    else if (ch === ',' && depth === 0) {
      parts.push(current.trim());
      current = '';
      continue;
    }
    current += ch;
  }
  if (current.trim()) parts.push(current.trim());

  for (const part of parts) {
    if (part.startsWith('...') || part.startsWith('{') || part.startsWith('[')) continue;
    const m = part.match(/^(\w+)\s*(\?)?\s*(?::\s*([^=]+))?\s*(?:=.*)?$/);
    if (!m) continue;
    const name = m[1];
    const optional = !!m[2] || part.includes('=');
    const tsType = m[3] ? m[3].trim() : null;
    properties[name] = { type: mapType(tsType) };
    if (!optional) required.push(name);
  }
  return { properties, required };
}

function getJSDocAbove(source, matchIndex) {
  const before = source.substring(0, matchIndex);
  const lines = before.split('\n');
  let docLines = [];
  let inDoc = false;
  for (let i = lines.length - 1; i >= 0; i--) {
    const line = lines[i].trim();
    if (!inDoc) {
      if (line === '' || line.startsWith('//')) continue;
      if (line.endsWith('*/')) {
        inDoc = true;
        docLines.unshift(line);
        if (line.startsWith('/**')) break;
        continue;
      }
      break;
    }
    docLines.unshift(line);
    if (line.startsWith('/**')) break;
  }
  if (docLines.length === 0) return '';
  const raw = docLines.join('\n');
  const content = raw
    .replace(/^\/\*\*\s*/, '')
    .replace(/\s*\*\/\s*$/, '')
    .split('\n')
    .map(l => l.replace(/^\s*\*\s?/, ''))
    .filter(l => !l.startsWith('@'))
    .join(' ')
    .trim();
  return content;
}

const tools = [];

// Pattern 1: export function funcName(params): returnType
const exportFuncRe = /^export\s+(?:async\s+)?function\s+(\w+)\s*\(([^)]*)\)/gm;
let m;
while ((m = exportFuncRe.exec(source)) !== null) {
  const name = m[1];
  if (name.startsWith('_')) continue;
  const desc = getJSDocAbove(source, m.index);
  const { properties, required } = parseParams(m[2]);
  const schema = { type: 'object', properties };
  if (required.length > 0) schema.required = required;
  tools.push({
    name,
    description: desc || 'Tool: ' + name,
    input_schema: JSON.stringify(schema),
    risk: 'read_only',
    language,
  });
}

// Pattern 2: export const funcName = (params) =>
const exportConstRe = /^export\s+const\s+(\w+)\s*=\s*(?:async\s+)?(?:function\s*)?\(([^)]*)\)/gm;
while ((m = exportConstRe.exec(source)) !== null) {
  const name = m[1];
  if (name.startsWith('_')) continue;
  const desc = getJSDocAbove(source, m.index);
  const { properties, required } = parseParams(m[2]);
  const schema = { type: 'object', properties };
  if (required.length > 0) schema.required = required;
  tools.push({
    name,
    description: desc || 'Tool: ' + name,
    input_schema: JSON.stringify(schema),
    risk: 'read_only',
    language,
  });
}

// Pattern 3: export default function funcName(params)
const exportDefaultRe = /^export\s+default\s+(?:async\s+)?function\s+(\w+)\s*\(([^)]*)\)/gm;
while ((m = exportDefaultRe.exec(source)) !== null) {
  const name = m[1];
  if (name.startsWith('_')) continue;
  const desc = getJSDocAbove(source, m.index);
  const { properties, required } = parseParams(m[2]);
  const schema = { type: 'object', properties };
  if (required.length > 0) schema.required = required;
  tools.push({
    name,
    description: desc || 'Tool: ' + name,
    input_schema: JSON.stringify(schema),
    risk: 'read_only',
    language,
  });
}

// Pattern 4: module.exports.funcName = (async) function(params)
const moduleExportsRe = /module\.exports\.(\w+)\s*=\s*(?:async\s+)?function\s*\(([^)]*)\)/gm;
while ((m = moduleExportsRe.exec(source)) !== null) {
  const name = m[1];
  if (name.startsWith('_')) continue;
  const desc = getJSDocAbove(source, m.index);
  const { properties, required } = parseParams(m[2]);
  const schema = { type: 'object', properties };
  if (required.length > 0) schema.required = required;
  tools.push({
    name,
    description: desc || 'Tool: ' + name,
    input_schema: JSON.stringify(schema),
    risk: 'read_only',
    language,
  });
}

process.stdout.write(JSON.stringify(tools));
`

// tsExtractDepsScript is the Node.js script that parses import/require
// statements and returns a JSON array of top-level package names,
// filtering out relative imports.
const tsExtractDepsScript = `'use strict';
const fs = require('fs');

const filePath = process.argv[2];
const source = fs.readFileSync(filePath, 'utf-8');
const deps = new Set();

// ES module imports: import ... from 'package'
const importRe = /import\s+(?:[\s\S]*?\s+from\s+)?['"]([^'"]+)['"]/g;
let m;
while ((m = importRe.exec(source)) !== null) {
  const pkg = m[1];
  if (pkg.startsWith('.') || pkg.startsWith('/')) continue;
  if (pkg.startsWith('@')) {
    const parts = pkg.split('/');
    if (parts.length >= 2) deps.add(parts[0] + '/' + parts[1]);
  } else {
    deps.add(pkg.split('/')[0]);
  }
}

// CommonJS require: require('package')
const requireRe = /require\s*\(\s*['"]([^'"]+)['"]\s*\)/g;
while ((m = requireRe.exec(source)) !== null) {
  const pkg = m[1];
  if (pkg.startsWith('.') || pkg.startsWith('/')) continue;
  if (pkg.startsWith('@')) {
    const parts = pkg.split('/');
    if (parts.length >= 2) deps.add(parts[0] + '/' + parts[1]);
  } else {
    deps.add(pkg.split('/')[0]);
  }
}

process.stdout.write(JSON.stringify(Array.from(deps).sort()));
`

// nodeBuiltinModules lists Node.js built-in modules that should be
// filtered out when inferring third-party dependencies.
var nodeBuiltinModules = map[string]bool{
	"assert":         true,
	"buffer":         true,
	"child_process":  true,
	"cluster":        true,
	"console":        true,
	"constants":      true,
	"crypto":         true,
	"dgram":          true,
	"dns":            true,
	"domain":         true,
	"events":         true,
	"fs":             true,
	"http":           true,
	"http2":          true,
	"https":          true,
	"module":         true,
	"net":            true,
	"os":             true,
	"path":           true,
	"perf_hooks":     true,
	"process":        true,
	"querystring":    true,
	"readline":       true,
	"repl":           true,
	"stream":         true,
	"string_decoder": true,
	"sys":            true,
	"timers":         true,
	"tls":            true,
	"tty":            true,
	"url":            true,
	"util":           true,
	"v8":             true,
	"vm":             true,
	"wasi":           true,
	"worker_threads": true,
	"zlib":           true,
}

// CanHandle returns true for .ts, .js, .mjs, .cjs files, excluding
// .d.ts declaration files, node_modules directories, and package.json.
func (ts *TypeScriptIntrospector) CanHandle(filePath string) bool {
	// Normalize to forward slashes for consistent matching.
	normalized := filepath.ToSlash(filePath)

	// Exclude node_modules paths.
	if strings.Contains(normalized, "node_modules/") {
		return false
	}

	base := filepath.Base(filePath)

	// Exclude package.json.
	if base == "package.json" {
		return false
	}

	// Exclude TypeScript declaration files.
	if strings.HasSuffix(base, ".d.ts") {
		return false
	}

	ext := filepath.Ext(filePath)
	switch ext {
	case ".ts", ".js", ".mjs", ".cjs":
		return true
	default:
		return false
	}
}

// tsExtractToolResult mirrors the JSON structure returned by the Node.js script.
type tsExtractToolResult struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema string `json:"input_schema"`
	Risk        string `json:"risk"`
	Language    string `json:"language"`
}

// writeScriptToTemp writes the given script content to a temporary file and
// returns its path. The caller is responsible for removing the file.
func writeScriptToTemp(script string) (string, error) {
	f, err := os.CreateTemp("", "foldermcp-*.js")
	if err != nil {
		return "", fmt.Errorf("create temp script: %w", err)
	}
	if _, err := f.WriteString(script); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("write temp script: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("close temp script: %w", err)
	}
	return f.Name(), nil
}

// ExtractTools runs the Node.js regex-based parser on the given file and
// returns the discovered tool metadata.
func (ts *TypeScriptIntrospector) ExtractTools(ctx context.Context, filePath string) ([]ToolMetadata, error) {
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		return nil, fmt.Errorf("node not found in PATH: %w", err)
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	scriptPath, err := writeScriptToTemp(tsExtractToolsScript)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(scriptPath) }()

	cmd := exec.CommandContext(ctx, nodeBin, scriptPath, absPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("node extract tools failed: %w\noutput: %s", err, string(out))
	}

	var results []tsExtractToolResult
	if err := json.Unmarshal(out, &results); err != nil {
		return nil, fmt.Errorf("parse node output: %w\nraw: %s", err, string(out))
	}

	tools := make([]ToolMetadata, len(results))
	for i, r := range results {
		tools[i] = ToolMetadata{
			Name:        r.Name,
			SourceFile:  absPath,
			Description: r.Description,
			InputSchema: r.InputSchema,
			Risk:        r.Risk,
			Language:    r.Language,
		}
	}
	return tools, nil
}

// InferDependencies runs the Node.js import-extraction script and filters
// out Node.js built-in modules, returning only third-party dependencies.
func (ts *TypeScriptIntrospector) InferDependencies(ctx context.Context, filePath string) ([]Dependency, error) {
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		return nil, fmt.Errorf("node not found in PATH: %w", err)
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	scriptPath, err := writeScriptToTemp(tsExtractDepsScript)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(scriptPath) }()

	cmd := exec.CommandContext(ctx, nodeBin, scriptPath, absPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("node extract deps failed: %w\noutput: %s", err, string(out))
	}

	var imports []string
	if err := json.Unmarshal(out, &imports); err != nil {
		return nil, fmt.Errorf("parse node output: %w\nraw: %s", err, string(out))
	}

	var deps []Dependency
	for _, imp := range imports {
		name := strings.TrimSpace(imp)
		if name == "" {
			continue
		}
		// Filter out node: prefixed built-ins.
		if strings.HasPrefix(name, "node:") {
			continue
		}
		if nodeBuiltinModules[name] {
			continue
		}
		deps = append(deps, Dependency{
			ImportName: name,
		})
	}
	return deps, nil
}
