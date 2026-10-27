## dirdoc

This repository describes its directories and files with dirdoc. The descriptions live in the `.dirdoc/` directory of each documented directory.

### Reading

- `dirdoc <dir>` shows a directory's summary, tree and the table of contents of each file; `dirdoc <file>` shows a file in detail; `dirdoc '<file>#<id>'` shows one type or function. Add `--brief` for less, `--full` for every document, `-f json` for JSON.
- `[[…]]` is a reference. Pass what is inside to `dirdoc '…'` to open it.
- Search with `dirdoc search <words>`. The files in `.dirdoc/` are compressed, so grep cannot search them.
- A view that starts with `> stale:` describes an older version of the file: check the code before relying on it.
- `dirdoc status` lists which directories and files are documented, stale or undocumented.
- Never read or write the files in `.dirdoc/` directly.

### Writing

When you change code, update its description too.

- To change part of a document, write only the changes and apply them with `--merge`. Entries are matched by id (or by the id derived from the name); `delete = true` removes one.

  ```bash
  dirdoc compile src/main.rs --merge - <<'EOF'
  [[functions]]
  name = "check_stdout()"
  summary = "Tell whether stdout is a terminal"
  EOF
  ```

- To rewrite a document: `dirdoc export <target> > /tmp/x.toml`, edit it, then `dirdoc compile <target> /tmp/x.toml`. When there is no description yet, export prints a template.
- Name methods `Type.method()` (in Rust `Type::method()`), as code analysis names them. In the languages dirdoc analyzes (Go, Rust, TypeScript, JavaScript, Java, Kotlin, Python, shell), `dirdoc <file>` then shows each entry's line and signature, and lists the symbols without an entry under Undocumented.
- When you changed a file whose description is still accurate, run `dirdoc touch <file>` so it is no longer shown as stale.
- Rename or move documented files with `dirdoc mv <old> <new>`: the description moves along and references are rewritten. Remove descriptions with `dirdoc rm <path>` or `dirdoc rm '<file>#<id>'`.
- Finish with `dirdoc check`.

### The source format (TOML)

A file (PFD):

```toml
summary = "What this file is for"

[[types]]                      # classes, structs, ...
name = "Config"
summary = "One-line description"

[[functions]]
name = "check_stdout()"        # the id is the part before "(", "<" or "[" (or set id = "...")
summary = "One-line description"
document = '''
Markdown body. Reference others with [[#MAX_AGENTS]], [[other.rs#parse]] or [[dir/]].
'''

[[variables]]
name = "MAX_AGENTS"
summary = "What it is for"
```

A directory (DSD) has `summary`, `tree` (in the style of the `tree` command; `[[#id]]` links a line to an entry and text after ` # ` is a note) and `[[dsd]]` entries (`id`, `name`, `document`).
