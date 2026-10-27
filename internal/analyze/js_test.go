package analyze

import "testing"

// tsTemplate is a template literal whose substitutions hold a '}' in a string and a nested template.
const tsTemplate = "`a ${ \"}\" } b ${ `${x}` }`"

const tsSource = `#!/usr/bin/env node
import { readFile } from "fs";
export * from "./other";

/** Upper bound. */
export const MAX_AGENTS = 8, MIN_AGENTS: number = 1;
let counter = 0
const re = /[/}]+/g, half = counter / 2 / 1;
const { a, b } = load();

export default class Pool<T extends { id: string }> extends Base implements Runner {
  static readonly DEFAULT = 4;
  #secret = 1;
  private workers: T[] = [];
  handler = (e: Event): void => { this.fire(e) };
  constructor(private size: number) { super(); }
  get size(): number { return this.workers.length }
  static create<T>(): Pool<T>;
  static create<T>(n?: number): Pool<T> { return new Pool(n ?? 1) }
  async *drain(): AsyncGenerator<T> {}
  ["computed"]() {}
  'quoted-name'() {}
}

export interface Runner {
  run(): void;
}

export enum Kind { A = 1, B }
const enum Flag { X }
export type Handler<T> = (value: T) => void;
type Point = { x: number; y: number };

export function spawn(n: number,
  opts?: Options,
): Pool<Worker> {
  const inner = () => {};
  return new Pool(n);
}

export async function* stream() {}

function overload(x: string): string;
function overload(x: any) { return x; }

export const handle = async (req: Request): Promise<Response> => {
  return fetch(req);
};
var tpl = ` + tsTemplate + `;
declare function ambient(): void;
declare module "x" { export const hidden: number; }
abstract class Shape { abstract area(): number; }
export default function () {}
`

func TestTypeScriptSymbols(t *testing.T) {
	checkSymbols(t, "pool.ts", tsSource, []string{
		"variable MAX_AGENTS 6: export const MAX_AGENTS = 8",
		"variable MIN_AGENTS 6: const MIN_AGENTS: number = 1",
		"variable counter 7: let counter = 0",
		"variable re 8: const re = /[/}]+/g",
		"variable half 8: const half = counter / 2 / 1",
		"type Pool 11: export default class Pool<T extends { id: string }> extends Base implements Runner { ... }",
		"variable Pool.DEFAULT 12: static readonly DEFAULT = 4",
		"function Pool.handler 15: handler = (e: Event): void => ...",
		"function Pool.constructor 16: constructor(private size: number)",
		"function Pool.size 17: get size(): number",
		"function Pool.create 18: static create<T>(): Pool<T>",
		"function Pool.drain 20: async *drain(): AsyncGenerator<T>",
		"function Pool.quoted-name 22: 'quoted-name'()",
		"type Runner 25: export interface Runner { ... }",
		"type Kind 29: export enum Kind { ... }",
		"type Flag 30: const enum Flag { ... }",
		"type Handler 31: export type Handler<T> = (value: T) => void",
		"type Point 32: type Point = { x: number; y: number }",
		"function spawn 34: export function spawn(n: number, opts?: Options): Pool<Worker>",
		"function stream 41: export async function* stream()",
		"function overload 43: function overload(x: string): string",
		"function handle 46: export const handle = async (req: Request): Promise<Response> => ...",
		"variable tpl 49: var tpl = " + tsTemplate,
		"function ambient 50: declare function ambient(): void",
		"type Shape 52: abstract class Shape { ... }",
		"function Shape.area 52: abstract area(): number",
	})
}

const jsSource = `'use strict';
const React = require("react");

export function App({ items }) {
  return (
    <ul className="list" data-x='{'>
      {items.map(i => <li key={i}>Don't {i} // not a comment</li>)}
      <Empty />
    </ul>
  );
}

export class Store {
  static instance = null
  count = 0
  increment() { this.count++ }
  static get default() { return Store.instance }
  async load() {}
  static {
    Store.instance = new Store()
  }
}

module.exports = { App };
export const double = x => x * 2
export let a = 1, b = function named() {}
const obj = {
  method() {}
}
var last
`

func TestJavaScriptSymbols(t *testing.T) {
	checkSymbols(t, "app.js", jsSource, []string{
		`variable React 2: const React = require("react")`,
		"function App 4: export function App({ items })",
		"type Store 13: export class Store { ... }",
		"variable Store.instance 14: static instance = null",
		"function Store.increment 16: increment()",
		"function Store.default 17: static get default()",
		"function Store.load 18: async load()",
		"function double 25: export const double = x => ...",
		"variable a 26: export let a = 1",
		"function b 26: let b = function named()",
		"variable obj 27: const obj = { method() {} }",
		"variable last 30: var last",
	})
}

const tsxSource = `import React from "react";

type Props<T> = { items: T[]; render?: (t: T) => JSX.Element };

export const List = <T,>({ items }: Props<T>) => (
  <>
    {items.length > 0 ? items.map((t, i) => <Row key={i} value={t} />) : <p>Nothing's here {"}"}</p>}
  </>
);

export function Row<T extends object>(props: { value: T }) {
  return <div title="a > b">{String(props.value)}</div>;
}

export default function Page() {
  const cmp = 1 < 2;
  return <List items={[1, 2]} />;
}
`

func TestTSXSymbols(t *testing.T) {
	checkSymbols(t, "view.tsx", tsxSource, []string{
		"type Props 3: type Props<T> = { items: T[]; render?: (t: T) => JSX.Element }",
		"function List 5: export const List = <T,>({ items }: Props<T>) => ...",
		"function Row 11: export function Row<T extends object>(props: { value: T })",
		"function Page 15: export default function Page()",
	})
}
