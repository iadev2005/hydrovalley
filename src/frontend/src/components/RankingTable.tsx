import type { RankingView } from "../logic/reports.ts";

export interface RankingTableProps {
  hidden?: boolean;
  ranking: RankingView | null;
  statusText: string;
  onRefresh: () => void;
}

export function RankingTable({
  hidden = false,
  ranking,
  statusText,
  onRefresh,
}: RankingTableProps) {
  return (
    <section id="ranking-panel" aria-labelledby="ranking-title" hidden={hidden}>
      <h2 id="ranking-title">Ranking</h2>
      <p className="meta">
        Los diez que más hojas han ganado.{" "}
        <button id="refresh" type="button" onClick={onRefresh}>
          Actualizar
        </button>
      </p>
      <table id="ranking-table">
        <thead>
          <tr>
            <th scope="col">Puesto</th>
            <th scope="col">Jugador</th>
            <th scope="col">Hojas ganadas</th>
          </tr>
        </thead>
        <tbody id="ranking-body">
          {ranking?.rows.map((row) => (
            <tr key={`${row.rank}-${row.username}`} className={row.isMe ? "me" : undefined}>
              <td>{row.rank}</td>
              <td>{row.username}</td>
              <td>{row.leaves}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <p className="meta" id="ranking-status" role="status">
        {statusText}
      </p>
    </section>
  );
}
