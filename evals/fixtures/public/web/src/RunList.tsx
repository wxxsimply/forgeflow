export function RunList({ runs }: { runs: Array<{ id: string; name: string }> }) {
  if (runs.length === 0) {
    return <div />;
  }
  return <ul>{runs.map((run) => <li key={run.id}>{run.name}</li>)}</ul>;
}
