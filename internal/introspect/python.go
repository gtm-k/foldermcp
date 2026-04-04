package introspect

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/foldermcp/foldermcp/internal/pythonrt"
)

// PythonIntrospector extracts tool metadata from Python source files
// by running Python AST parsing via subprocess.
type PythonIntrospector struct{}

// pythonStdlibModules lists common Python standard library module names
// so they can be filtered out when inferring third-party dependencies.
var pythonStdlibModules = map[string]bool{
	"abc": true, "argparse": true, "ast": true, "asyncio": true,
	"base64": true, "bisect": true, "builtins": true,
	"calendar": true, "cgi": true, "cmd": true, "code": true,
	"codecs": true, "collections": true, "concurrent": true,
	"configparser": true, "contextlib": true, "copy": true, "csv": true,
	"ctypes": true, "dataclasses": true, "datetime": true, "decimal": true,
	"difflib": true, "dis": true, "distutils": true, "doctest": true,
	"email": true, "enum": true, "errno": true,
	"fileinput": true, "fnmatch": true, "fractions": true, "ftplib": true,
	"functools": true, "gc": true, "getpass": true, "gettext": true,
	"glob": true, "gzip": true, "hashlib": true, "heapq": true,
	"hmac": true, "html": true, "http": true,
	"imaplib": true, "importlib": true, "inspect": true, "io": true,
	"ipaddress": true, "itertools": true, "json": true, "keyword": true,
	"linecache": true, "locale": true, "logging": true, "lzma": true,
	"mailbox": true, "math": true, "mimetypes": true, "mmap": true,
	"multiprocessing": true, "numbers": true, "operator": true, "os": true,
	"pathlib": true, "pickle": true, "pipes": true, "pkgutil": true,
	"platform": true, "pprint": true, "profile": true,
	"queue": true, "quopri": true, "random": true, "re": true,
	"readline": true, "reprlib": true, "resource": true,
	"runpy": true, "sched": true, "secrets": true, "select": true,
	"shelve": true, "shlex": true, "shutil": true, "signal": true,
	"site": true, "smtplib": true, "socket": true, "sqlite3": true,
	"ssl": true, "stat": true, "statistics": true, "string": true,
	"struct": true, "subprocess": true, "sys": true, "syslog": true,
	"tarfile": true, "tempfile": true, "textwrap": true, "threading": true,
	"time": true, "timeit": true, "tkinter": true, "token": true,
	"tokenize": true, "traceback": true, "tracemalloc": true, "types": true,
	"typing": true, "unicodedata": true, "unittest": true, "urllib": true,
	"uu": true, "uuid": true, "venv": true, "warnings": true,
	"wave": true, "weakref": true, "webbrowser": true, "xml": true,
	"xmlrpc": true, "zipfile": true, "zipimport": true, "zlib": true,
}

// extractToolsScript is the Python script that parses a file's AST and
// returns a JSON array of tool metadata. It detects both plain functions
// (using docstrings) and functions decorated with @tool(...) from the
// foldermcp_decorator module.
const extractToolsScript = `
import ast, json, sys

type_map = {
    'int': 'integer',
    'float': 'number',
    'str': 'string',
    'bool': 'boolean',
    'list': 'array',
    'List': 'array',
    'dict': 'object',
    'Dict': 'object',
}

def get_type_name(annotation):
    if annotation is None:
        return 'string'
    if isinstance(annotation, ast.Name):
        return type_map.get(annotation.id, 'string')
    if isinstance(annotation, ast.Attribute):
        return type_map.get(annotation.attr, 'string')
    if isinstance(annotation, ast.Subscript):
        if isinstance(annotation.value, ast.Name):
            return type_map.get(annotation.value.id, 'string')
    return 'string'

def get_decorator_info(node):
    """Check if the function has a @tool(...) decorator and extract its kwargs."""
    for dec in node.decorator_list:
        dec_name = None
        if isinstance(dec, ast.Call):
            if isinstance(dec.func, ast.Name) and dec.func.id == 'tool':
                dec_name = 'tool'
            elif isinstance(dec.func, ast.Attribute) and dec.func.attr == 'tool':
                dec_name = 'tool'
        elif isinstance(dec, ast.Name) and dec.id == 'tool':
            return {}  # bare @tool with no arguments
        if dec_name == 'tool':
            kwargs = {}
            for kw in dec.keywords:
                if kw.arg and isinstance(kw.value, ast.Constant):
                    kwargs[kw.arg] = kw.value.value
            return kwargs
    return None

def extract_tools(filepath):
    with open(filepath, 'r', encoding='utf-8') as f:
        source = f.read()
    tree = ast.parse(source, filename=filepath)
    tools = []
    for node in ast.iter_child_nodes(tree):
        if not isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
            continue
        if node.name.startswith('_'):
            continue

        # Check for @tool decorator
        dec_info = get_decorator_info(node)
        has_tool_decorator = dec_info is not None
        if dec_info is None:
            dec_info = {}

        docstring = ast.get_docstring(node)

        # Determine description: decorator kwarg > docstring > default
        if 'description' in dec_info:
            description = dec_info['description']
        elif docstring:
            description = docstring
        else:
            description = 'Tool: ' + node.name

        # Detect async functions
        is_async = isinstance(node, ast.AsyncFunctionDef)

        # Determine risk: decorator kwarg > heuristic > default
        risk = dec_info.get('risk', 'read_only')

        # Heuristic risk classification from function name (only when no
        # explicit risk was provided via decorator)
        if 'risk' not in dec_info:
            name_lower = node.name.lower()
            if any(w in name_lower for w in ['delete', 'remove', 'drop', 'destroy', 'purge', 'truncate']):
                risk = "destructive"
            elif any(w in name_lower for w in ['send', 'write', 'update', 'create', 'insert', 'post', 'put', 'push', 'deploy', 'execute', 'run', 'modify', 'set', 'notify']):
                risk = "side_effects"
            elif any(w in name_lower for w in ['fetch', 'download', 'upload', 'request', 'call', 'connect']):
                risk = "network"

        # Extract return type annotation
        ret_type = ""
        if node.returns:
            ret_type = ast.dump(node.returns)
            # Simplify common types
            for py_type, label in [("int", "int"), ("float", "float"), ("str", "str"), ("bool", "bool"), ("list", "list"), ("dict", "dict")]:
                if py_type in ret_type.lower():
                    ret_type = label
                    break

        # Append return type to description if available
        if ret_type:
            description = f"{description} Returns: {ret_type}."

        # Determine tool name: decorator kwarg > function name
        tool_name = dec_info.get('name', node.name)

        args = node.args
        properties = {}
        required = []

        num_args = len(args.args)
        num_defaults = len(args.defaults)
        first_default_idx = num_args - num_defaults

        for i, arg in enumerate(args.args):
            if arg.arg == 'self' or arg.arg == 'cls':
                continue
            json_type = get_type_name(arg.annotation)
            properties[arg.arg] = {'type': json_type}
            if i < first_default_idx:
                required.append(arg.arg)

        schema = {
            'type': 'object',
            'properties': properties,
        }
        if required:
            schema['required'] = required

        tools.append({
            'name': tool_name,
            'description': description,
            'input_schema': json.dumps(schema),
            'risk': risk,
            'language': 'python',
            'is_async': is_async,
        })
    print(json.dumps(tools))

extract_tools(sys.argv[1])
`

// extractDepsScript is the Python script that parses imports from a file's AST.
const extractDepsScript = `
import ast, json, sys

def extract_deps(filepath):
    with open(filepath, 'r', encoding='utf-8') as f:
        source = f.read()
    tree = ast.parse(source, filename=filepath)
    imports = set()
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            for alias in node.names:
                top = alias.name.split('.')[0]
                imports.add(top)
        elif isinstance(node, ast.ImportFrom):
            if node.module:
                top = node.module.split('.')[0]
                imports.add(top)
    print(json.dumps(sorted(imports)))

extract_deps(sys.argv[1])
`

// findPython delegates to the shared pythonrt package.
func findPython() (string, error) {
	return pythonrt.FindPython()
}

// CanHandle returns true for files with a .py extension.
func (p *PythonIntrospector) CanHandle(filePath string) bool {
	return filepath.Ext(filePath) == ".py"
}

// extractToolResult mirrors the JSON structure returned by the Python script.
type extractToolResult struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema string `json:"input_schema"`
	Risk        string `json:"risk"`
	Language    string `json:"language"`
	IsAsync     bool   `json:"is_async"`
}

// ExtractTools runs the Python AST parsing script on the given file and
// returns the discovered tool metadata.
func (p *PythonIntrospector) ExtractTools(ctx context.Context, filePath string) ([]ToolMetadata, error) {
	pythonBin, err := findPython()
	if err != nil {
		return nil, err
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	cmd := exec.CommandContext(ctx, pythonBin, "-c", extractToolsScript, absPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("python extract tools failed: %w\noutput: %s", err, string(out))
	}

	var results []extractToolResult
	if err := json.Unmarshal(out, &results); err != nil {
		return nil, fmt.Errorf("parse python output: %w\nraw: %s", err, string(out))
	}

	tools := make([]ToolMetadata, len(results))
	for i, r := range results {
		desc := r.Description
		risk := r.Risk
		if r.IsAsync {
			desc = "(async) " + desc
			if risk == "read_only" {
				risk = "side_effects"
			}
		}
		tools[i] = ToolMetadata{
			Name:        r.Name,
			SourceFile:  absPath,
			Description: desc,
			InputSchema: r.InputSchema,
			Risk:        risk,
			Language:    r.Language,
		}
	}
	return tools, nil
}

// InferDependencies runs the Python AST import-extraction script and filters
// out standard library modules, returning only third-party dependencies.
func (p *PythonIntrospector) InferDependencies(ctx context.Context, filePath string) ([]Dependency, error) {
	pythonBin, err := findPython()
	if err != nil {
		return nil, err
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	cmd := exec.CommandContext(ctx, pythonBin, "-c", extractDepsScript, absPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("python extract deps failed: %w\noutput: %s", err, string(out))
	}

	var imports []string
	if err := json.Unmarshal(out, &imports); err != nil {
		return nil, fmt.Errorf("parse python output: %w\nraw: %s", err, string(out))
	}

	var deps []Dependency
	for _, imp := range imports {
		name := strings.TrimSpace(imp)
		if name == "" {
			continue
		}
		if pythonStdlibModules[name] {
			continue
		}
		deps = append(deps, Dependency{
			ImportName: name,
		})
	}
	return deps, nil
}
