package main

import "testing"

func TestGoTestDocs(t *testing.T) {
	content := "package p\n\nimport \"testing\"\n\n// TestA proves A.\nfunc TestA(t *testing.T) {\n\t// Setup holds a condition.\n\tt.Log(1)\n}\n\nfunc BenchmarkB(b *testing.B) {} // Measures B.\n\n// FuzzC fuzzes C.\nfunc FuzzC(f *testing.F) {}\n\n// helper sets up.\nfunc helper() {}\n\n//go:noinline\nfunc TestD(t *testing.T) {}\n"
	expectEqual(t, "go", violationsOf(t, "p_test.go", content), sorted("5:test-doc", "11:test-doc", "13:test-doc"))
	expectEqual(t, "outside test files", violationsOf(t, "p.go", "package p\n\n// TestA is a name.\nfunc TestA() {}\n"), nil)
}

func TestRustTestDocs(t *testing.T) {
	content := "#[cfg(test)]\nmod tests {\n    // Above.\n    #[test]\n    // Between.\n    #[should_panic]\n    fn a() {} // On.\n\n    #[tokio::test(flavor = \"multi_thread\")]\n    async fn b() {\n        // Inside holds a condition.\n    }\n\n    // Helper sets up.\n    fn helper() {}\n}\n"
	expectEqual(t, "rust", violationsOf(t, "lib.rs", content), sorted("3:test-doc", "5:test-doc", "7:test-doc"))
}

func TestTypeScriptTestDocs(t *testing.T) {
	content := "// Suite.\ndescribe('s', () => {\n  // Case.\n  it('a', () => {\n    // Inside holds a condition.\n  });\n  it.each([1])('b %i', () => {}); // On.\n  // Setup holds a condition.\n  beforeEach(() => {});\n  test.describe.serial('c', () => {});\n});\n"
	expectEqual(t, "ts", violationsOf(t, "a.test.ts", content), sorted("1:test-doc", "3:test-doc", "7:test-doc"))
	expectEqual(t, "spec", violationsOf(t, "e2e/a.spec.ts", "// Case.\ntest('a', async () => {});\n"), []string{"1:test-doc"})
	expectEqual(t, "outside test files", violationsOf(t, "a.ts", "// Case.\ntest('a');\n"), nil)
}
