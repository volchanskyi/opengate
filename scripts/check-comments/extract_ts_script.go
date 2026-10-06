package main

const typescriptScript = `
const ts = require(process.argv[1]);
let input = '';
process.stdin.setEncoding('utf8');
process.stdin.on('data', (chunk) => { input += chunk; });
process.stdin.on('end', () => {
  const request = JSON.parse(input);
  process.stdout.write(JSON.stringify(request.files.map((file) => extract(file, request.units))));
});
function kindOf(name) {
  if (/\.tsx$/.test(name)) return ts.ScriptKind.TSX;
  if (/\.jsx$/.test(name)) return ts.ScriptKind.JSX;
  if (/\.[cm]?js$/.test(name)) return ts.ScriptKind.JS;
  if (/\.json$/.test(name)) return ts.ScriptKind.JSON;
  return ts.ScriptKind.TS;
}
function isJSDoc(node) {
  return node.kind >= ts.SyntaxKind.FirstJSDocNode && node.kind <= ts.SyntaxKind.LastJSDocNode;
}
function normaliseJsxText(text) {
  return text.split('\n').map((line) => line.trim()).filter((line) => line !== '').join(' ');
}
function extract(file, withUnits) {
  const text = file.content;
  const source = ts.createSourceFile(file.path, text, ts.ScriptTarget.Latest, true, kindOf(file.path));
  const seen = new Set();
  const ranges = [];
  const jsxText = [];
  const units = [];
  const add = (range) => {
    if (seen.has(range.pos)) return;
    seen.add(range.pos);
    ranges.push([range.pos, range.end]);
  };
  const visit = (node, silent) => {
    if (isJSDoc(node)) return;
    const quiet = silent || (node.kind === ts.SyntaxKind.JsxExpression && !node.expression);
    const children = node.getChildren(source);
    if (children.length > 0) {
      for (const child of children) visit(child, quiet);
      return;
    }
    if (node.kind === ts.SyntaxKind.JsxText) {
      jsxText.push([node.pos, node.end]);
      const normal = normaliseJsxText(text.slice(node.pos, node.end));
      if (withUnits && normal !== '') units.push('JsxText ' + normal);
      return;
    }
    (ts.getLeadingCommentRanges(text, node.pos) || []).forEach(add);
    (ts.getTrailingCommentRanges(text, node.end) || []).forEach(add);
    if (withUnits && !quiet && node.kind !== ts.SyntaxKind.EndOfFileToken) {
      units.push(node.kind + ' ' + text.slice(node.getStart(source), node.end));
    }
  };
  visit(source, false);
  const inJsxText = (pos) => jsxText.some(([start, end]) => pos >= start && pos < end);
  const comments = ranges.filter(([pos]) => !inJsxText(pos)).sort((a, b) => a[0] - b[0]);
  return { comments, units };
}
`
