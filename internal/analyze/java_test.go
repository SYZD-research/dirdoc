package analyze

import "testing"

const javaSource = `package com.example.pool;

import java.util.List;

/** A pool. */
@Deprecated(since = "1")
public final class Pool<T extends Runnable> extends Base implements AutoCloseable {
    public static final int MAX_AGENTS = 8;
    private static String name = "pool", other;
    private final List<T> workers = new ArrayList<>();

    static { init(); }

    public Pool() { this(4); }

    @SafeVarargs
    public Pool(int size, T... initial) {
        String s = "}";
        char c = '}';
    }

    public <R> R submit(Callable<R> task) throws Exception {
        return null;
    }

    public void close() {}
    public void close(boolean force) {}

    abstract static class Worker implements Runnable {
        abstract void work();
    }

    enum State { IDLE, RUNNING { void run() {} }; void reset() {} }

    record Point(int x, int y) {
        Point {
            if (x < 0) throw new IllegalArgumentException();
        }
        static Point origin() { return new Point(0, 0); }
    }
}

interface Runner {
    int LIMIT = 3;
    void run();
    default String name() { return """
        text block with } and "quotes"
        """; }
}

@interface Marker {
    String value() default "";
}

sealed interface Shape permits Circle {}
non-sealed class Circle implements Shape {}
`

func TestJavaSymbols(t *testing.T) {
	checkSymbols(t, "Pool.java", javaSource, []string{
		"type Pool 7: public final class Pool<T extends Runnable> extends Base implements AutoCloseable { ... }",
		"variable Pool.MAX_AGENTS 8: public static final int MAX_AGENTS = 8",
		`variable Pool.name 9: private static String name = "pool"`,
		"variable Pool.other 9: private static String other",
		"function Pool.Pool 14: public Pool()",
		"function Pool.submit 22: public <R> R submit(Callable<R> task) throws Exception",
		"function Pool.close 26: public void close()",
		"type Pool.Worker 29: abstract static class Worker implements Runnable { ... }",
		"function Pool.Worker.work 30: abstract void work()",
		"type Pool.State 33: enum State { ... }",
		"function Pool.State.reset 33: void reset()",
		"type Pool.Point 35: record Point(int x, int y) { ... }",
		"function Pool.Point.Point 36: Point",
		"function Pool.Point.origin 39: static Point origin()",
		"type Runner 43: interface Runner { ... }",
		"variable Runner.LIMIT 44: int LIMIT = 3",
		"function Runner.run 45: void run()",
		"function Runner.name 46: default String name()",
		"type Marker 51: @interface Marker { ... }",
		`function Marker.value 52: String value() default ""`,
		"type Shape 55: sealed interface Shape permits Circle { ... }",
		"type Circle 56: non-sealed class Circle implements Shape { ... }",
	})
}
