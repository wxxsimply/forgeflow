import { NavLink } from 'react-router-dom';

export function RunSubnav({ runId }: { runId: string }) {
  return <nav className="run-subnav" aria-label="Run 详情导航">
    <NavLink end to={`/runs/${runId}`}>概览</NavLink>
    <NavLink to={`/runs/${runId}/artifacts`}>产物与 Diff</NavLink>
    <NavLink to={`/runs/${runId}/trace`}>调用审计</NavLink>
    <NavLink to={`/runs/${runId}/report`}>最终报告</NavLink>
  </nav>;
}
