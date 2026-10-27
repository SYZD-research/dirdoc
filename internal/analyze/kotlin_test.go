package analyze

import "testing"

const kotlinSource = `@file:JvmName("Pool")
package com.example.pool

import kotlin.math.max

/** Upper bound. */
const val MAX_AGENTS = 8
val greeting: String = "hello ${name.uppercase() + "}"} done"
var counter = 0
    private set
val lazyValue by lazy { compute() }

typealias Handler<T> = (T) -> Unit

data class Point(val x: Int, val y: Int = 0)

open class Pool<T : Any>(
    private val size: Int,
) : Base(), Runner {
    val workers = mutableListOf<T>()

    constructor() : this(4)

    init { check(size > 0) }

    fun spawn(n: Int): List<T> {
        val s = """raw } string"""
        return emptyList()
    }

    override fun run() = Unit

    inner class Worker

    companion object Factory {
        const val DEFAULT = 4
        fun create(): Pool<Any> = Pool(DEFAULT)
    }
}

interface Runner {
    fun run()
}

fun interface Callback { fun call() }

object Registry {
    val items = mutableMapOf<String, Int>()
    fun register(name: String) {}
}

enum class Color(val rgb: Int) {
    RED(0xFF0000) { override fun label() = "red" },
    GREEN(0x00FF00);

    open fun label() = name
}

fun <T> List<T>.second(): T = this[1]
fun String?.orBlank(): String = this ?: ""
suspend fun fetch(
    url: String,
): String {
    return url
}

val Int.squared: Int
    get() = this * this

fun ` + "`quoted name`" + `() {}
fun ` + "`is-valid`" + `() = true
`

func TestKotlinSymbols(t *testing.T) {
	checkSymbols(t, "Pool.kt", kotlinSource, []string{
		"variable MAX_AGENTS 7: const val MAX_AGENTS = 8",
		`variable greeting 8: val greeting: String = "hello ${name.uppercase() + "}"} done"`,
		"variable counter 9: var counter = 0",
		"variable lazyValue 11: val lazyValue by lazy { compute() }",
		"type Handler 13: typealias Handler<T> = (T) -> Unit",
		"type Point 15: data class Point(val x: Int, val y: Int = 0)",
		"type Pool 17: open class Pool<T : Any>(private val size: Int) : Base(), Runner { ... }",
		"function Pool.constructor 22: constructor()",
		"function Pool.spawn 26: fun spawn(n: Int): List<T>",
		"function Pool.run 31: override fun run()",
		"type Pool.Worker 33: inner class Worker",
		"variable Pool.DEFAULT 36: const val DEFAULT = 4",
		"function Pool.create 37: fun create(): Pool<Any>",
		"type Runner 41: interface Runner { ... }",
		"function Runner.run 42: fun run()",
		"type Callback 45: fun interface Callback { ... }",
		"function Callback.call 45: fun call()",
		"type Registry 47: object Registry { ... }",
		"variable Registry.items 48: val items = mutableMapOf<String, Int>()",
		"function Registry.register 49: fun register(name: String)",
		"type Color 52: enum class Color(val rgb: Int) { ... }",
		"function Color.label 56: open fun label()",
		"function List.second 59: fun <T> List<T>.second(): T",
		"function String.orBlank 60: fun String?.orBlank(): String",
		"function fetch 61: suspend fun fetch(url: String): String",
		"variable Int.squared 67: val Int.squared: Int",
		"function is-valid 71: fun `is-valid`()",
	})
}
