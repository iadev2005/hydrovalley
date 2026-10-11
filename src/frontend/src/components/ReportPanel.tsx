import type { ReportScope, ReportView } from "../logic/reports.ts";

export interface ReportPanelProps {
  hidden?: boolean;
  report: ReportView | null;
  reportScope: ReportScope;
  statusText: string;
  onSelectScope: (scope: ReportScope) => void;
}

export function ReportPanel({
  hidden = false,
  report,
  reportScope,
  statusText,
  onSelectScope,
}: ReportPanelProps) {
  return (
    <section id="report-panel" aria-labelledby="report-title" hidden={hidden}>
      <h2 id="report-title">Reporte de las 3R</h2>
      <div role="tablist" aria-label="Alcance del reporte">
        <button
          id="tab-me"
          type="button"
          role="tab"
          aria-selected={reportScope === "me"}
          aria-controls="report-body"
          onClick={() => onSelectScope("me")}
        >
          Mis 3R
        </button>
        <button
          id="tab-global"
          type="button"
          role="tab"
          aria-selected={reportScope === "global"}
          aria-controls="report-body"
          onClick={() => onSelectScope("global")}
        >
          Valle
        </button>
      </div>
      <p className="meta" id="report-period">
        {report?.period ?? ""}
      </p>
      <table id="report-body" role="tabpanel">
        <tbody id="report-rows">
          {report?.rows.map((row) => (
            <tr key={row.key}>
              <th scope="row">{row.label}</th>
              <td>{row.value}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <p className="meta" id="report-note">
        {report?.note ?? ""}
      </p>
      <p className="meta" id="report-status" role="status">
        {statusText}
      </p>
    </section>
  );
}
