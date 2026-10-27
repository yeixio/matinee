// Version numbers: "v1.2.3" to [1, 2, 3], or [] for anything else.
export const semver = (v) => (v.match(/^v?(\d+)\.(\d+)\.(\d+)$/) ?? []).slice(1).map(Number)

export function compare(a, b) {
  const [x, y] = [semver(a), semver(b)]
  for (let i = 0; i < 3; i++) if (x[i] !== y[i]) return x[i] - y[i]
  return 0
}
