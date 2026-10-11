import { SystemDashboard } from "../components/SystemDashboard.tsx";
import { useSystemMetrics, type UseSystemMetricsOptions } from "../hooks/useSystemMetrics.ts";

export interface SystemAppProps {
  metricsOptions?: UseSystemMetricsOptions;
}

export function SystemApp({ metricsOptions }: SystemAppProps = {}) {
  const { metrics, error, lastUpdated } = useSystemMetrics(metricsOptions);

  return (
    <SystemDashboard
      metrics={metrics}
      error={error}
      lastUpdated={lastUpdated}
    />
  );
}
