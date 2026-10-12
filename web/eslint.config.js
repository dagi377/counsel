import tseslint from "typescript-eslint";

// ESLint flat config for web/ (npm run lint; make check runs it). Money is integer cents and rates are integer ppm, so float
// arithmetic is banned in web/src: render amounts only through kernel/money.ts formatCents.
export const noFloat = [
  { selector: "Literal[raw=/^\\d*\\.\\d+(e[+-]?\\d+)?$|^\\d+\\.$/i]", message: "No float literals: use integer cents or ppm." },
  { selector: "CallExpression[callee.name='parseFloat']", message: "No parseFloat: parse integer cents." },
  { selector: "MemberExpression[property.name='parseFloat']", message: "No Number.parseFloat: parse integer cents." },
  { selector: "MemberExpression[property.name='toFixed']", message: "No toFixed: format money with formatCents." },
  { selector: "MemberExpression[property.name='toPrecision']", message: "No toPrecision: format money with formatCents." },
  { selector: "MemberExpression[object.name='Intl'][property.name='NumberFormat']", message: "No Intl.NumberFormat: format money with formatCents." },
  { selector: "BinaryExpression[operator='/']", message: "No division: integer money math belongs on the server." },
  { selector: "AssignmentExpression[operator='/=']", message: "No division: integer money math belongs on the server." },
];

export default [
  {
    files: ["src/**/*.{ts,tsx}"],
    languageOptions: { parser: tseslint.parser },
    ignores: ["src/**/*.test.{ts,tsx}"],
    rules: {
      "no-restricted-syntax": ["error", ...noFloat],
    },
  },
];
