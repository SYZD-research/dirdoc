package analyze

import "testing"

const rustSource = `//! Worker pool.
use std::fmt;

/// Upper bound.
pub const MAX_AGENTS: usize = 8;
static mut COUNT: u32 = 0;

#[derive(Debug, Clone)]
pub struct Pool<T: Send> {
    workers: Vec<T>,
}

pub struct Unit;
struct Pair(i32, i32);

pub enum Kind { A, B(u8) }

pub trait Runner: Send {
    type Output;
    const LIMIT: u32 = 3;
    fn run(&self) -> Self::Output;
    fn name(&self) -> &'static str { "runner" }
}

impl<T: Send> Pool<T> {
    /// Creates a pool.
    pub fn new() -> Self { Pool { workers: Vec::new() } }

    pub async fn spawn(
        &mut self,
        n: usize, // how many
    ) -> Result<(), String>
    where
        T: Default,
    {
        let s = "}";
        let c = '}';
        let l: &'a str = r#"raw " string }"#;
        Ok(())
    }
}

impl<T: Send> fmt::Display for Pool<T> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result { write!(f, "pool") }
}

pub type Result<T> = std::result::Result<T, Error>;

macro_rules! square { ($x:expr) => { $x * $x }; }

pub(crate) const fn limit<'a>(s: &'a str) -> usize { s.len() }

/* a /* nested */ comment with fn hidden() {} */
fn r#match() {}

lazy_static! { static ref TABLE: Vec<u8> = vec![]; }

mod tests {
    #[test]
    fn it_works() {}
}

extern "C" { fn abs(x: i32) -> i32; }
`

func TestRustSymbols(t *testing.T) {
	checkSymbols(t, "pool.rs", rustSource, []string{
		"variable MAX_AGENTS 5: pub const MAX_AGENTS: usize = 8",
		"variable COUNT 6: static mut COUNT: u32 = 0",
		"type Pool 9: pub struct Pool<T: Send> { ... }",
		"type Unit 13: pub struct Unit",
		"type Pair 14: struct Pair(i32, i32)",
		"type Kind 16: pub enum Kind { ... }",
		"type Runner 18: pub trait Runner: Send { ... }",
		"type Runner::Output 19: type Output",
		"variable Runner::LIMIT 20: const LIMIT: u32 = 3",
		"function Runner::run 21: fn run(&self) -> Self::Output",
		"function Runner::name 22: fn name(&self) -> &'static str",
		"function Pool::new 27: pub fn new() -> Self",
		"function Pool::spawn 29: pub async fn spawn(&mut self, n: usize) -> Result<(), String> where T: Default",
		"type Result 47: pub type Result<T> = std::result::Result<T, Error>",
		"function square 49: macro_rules! square",
		"function limit 51: pub(crate) const fn limit<'a>(s: &'a str) -> usize",
		"function match 54: fn r#match()",
	})
}
