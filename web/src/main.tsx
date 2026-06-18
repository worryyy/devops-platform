import React, { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { Activity, Box, GitBranch, Package, Rocket, Server, Settings } from "lucide-react";
import { api } from "./api";
import { classForStatus } from "./status";
import type { BuildRecord, DashboardSummary, DeployRecord, Service, StageRecord } from "./types";
import "./styles.css";

type Route = {
  path: string;
  label: string;
  icon: React.ReactNode;
};

const routes: Route[] = [
  { path: "/dashboard", label: "Dashboard", icon: <Activity size={16} /> },
  { path: "/services", label: "Services", icon: <Server size={16} /> },
  { path: "/builds", label: "Builds", icon: <Package size={16} /> },
  { path: "/build", label: "Build", icon: <GitBranch size={16} /> },
  { path: "/deploys", label: "Deploys", icon: <Rocket size={16} /> },
  { path: "/deploy", label: "Deploy", icon: <Box size={16} /> },
  { path: "/settings/catalog", label: "Catalog", icon: <Settings size={16} /> }
];

function useAsync<T>(loader: () => Promise<T>, deps: React.DependencyList) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string>("");
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let canceled = false;
    setLoading(true);
    setError("");
    loader()
      .then((value) => {
        if (!canceled) setData(value);
      })
      .catch((err: Error) => {
        if (!canceled) setError(err.message);
      })
      .finally(() => {
        if (!canceled) setLoading(false);
      });
    return () => {
      canceled = true;
    };
  }, deps);

  return { data, error, loading };
}

function App() {
  const [path, setPath] = useState(window.location.pathname === "/" ? "/dashboard" : window.location.pathname);

  useEffect(() => {
    const onPopState = () => setPath(window.location.pathname);
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);

  function navigate(next: string) {
    window.history.pushState({}, "", next);
    setPath(next);
  }

  return (
    <div className="app">
      <aside>
        <div className="brand">CI/CD</div>
        <nav>
          {routes.map((route) => (
            <button key={route.path} className={path === route.path ? "active" : ""} onClick={() => navigate(route.path)}>
              {route.icon}
              <span>{route.label}</span>
            </button>
          ))}
        </nav>
      </aside>
      <main>
        {path === "/dashboard" && <Dashboard />}
        {path === "/services" && <Services navigate={navigate} />}
        {path.startsWith("/services/") && path !== "/services" && <ServiceDetails name={decodeURIComponent(path.split("/")[2] ?? "")} />}
        {path === "/builds" && <Builds navigate={navigate} />}
        {path.startsWith("/builds/") && path !== "/builds" && <BuildDetails id={decodeURIComponent(path.split("/")[2] ?? "")} />}
        {path === "/build" && <BuildForm />}
        {path === "/deploys" && <Deploys navigate={navigate} />}
        {path.startsWith("/deploys/") && path !== "/deploys" && <DeployDetails id={decodeURIComponent(path.split("/")[2] ?? "")} />}
        {path === "/deploy" && <DeployForm />}
        {path === "/settings/catalog" && <CatalogSettings />}
        {path === "/rollouts" && <Empty title="Rollouts" text="Argo Rollouts and Prometheus are not configured yet." />}
      </main>
    </div>
  );
}

function Dashboard() {
  const { data, error, loading } = useAsync<DashboardSummary>(() => api.summary(), []);
  if (loading) return <Loading title="Dashboard" />;
  if (error) return <ErrorState title="Dashboard" error={error} />;
  if (!data) return <Empty title="Dashboard" text="No dashboard data available." />;
  return (
    <section>
      <Header title="CI/CD Dashboard" subtitle="Aggregated from platform records in Postgres." />
      <div className="metric-grid">
        <Metric label="Today Builds" value={data.today_builds} />
        <Metric label="Today Deploys" value={data.today_deploys} />
        <Metric label="Build Success" value={`${Math.round(data.build_success_rate * 100)}%`} />
        <Metric label="Deploy Success" value={`${Math.round(data.deploy_success_rate * 100)}%`} />
        <Metric label="Running Builds" value={data.running_builds} />
        <Metric label="Running Deploys" value={data.running_deploys} />
        <Metric label="Failed Builds" value={data.recent_failed_builds} />
        <Metric label="Failed Deploys" value={data.recent_failed_deploys} />
      </div>
      <div className="columns">
        <RecordList title="Recent Builds" records={data.recent_builds} idKey="build_id" />
        <RecordList title="Recent Deploys" records={data.recent_deploys} idKey="deploy_id" />
      </div>
      <Panel title="Active Deploy Locks">
        {data.active_locks.length === 0 ? <p className="muted">No active deploy locks.</p> : <pre>{JSON.stringify(data.active_locks, null, 2)}</pre>}
      </Panel>
    </section>
  );
}

function Services({ navigate }: { navigate: (path: string) => void }) {
  const { data, error, loading } = useAsync(() => api.services(), []);
  if (loading) return <Loading title="Services" />;
  if (error) return <ErrorState title="Services" error={error} />;
  const services = data?.services ?? [];
  return (
    <section>
      <Header title="Services" subtitle="Loaded from service-catalog.yaml." />
      {services.length === 0 ? <Empty title="Services" text="No services configured." /> : (
        <div className="table">
          <div className="row head"><span>Name</span><span>Repo</span><span>Namespace</span><span>Image</span></div>
          {services.map((service) => {
            const env = service.environments[0];
            return (
              <button className="row clickable" key={service.name} onClick={() => navigate(`/services/${encodeURIComponent(service.name)}`)}>
                <span>{service.name}</span>
                <span>{env?.git.repo ?? "-"}</span>
                <span>{env?.namespace ?? "-"}</span>
                <span>{env?.image.repository ?? "-"}</span>
              </button>
            );
          })}
        </div>
      )}
    </section>
  );
}

function ServiceDetails({ name }: { name: string }) {
  const { data, error, loading } = useAsync<Service>(() => api.service(name), [name]);
  if (loading) return <Loading title={name} />;
  if (error) return <ErrorState title={name} error={error} />;
  if (!data) return <Empty title={name} text="Service not found." />;
  return (
    <section>
      <Header title={data.displayName || data.name} subtitle={`Owner: ${data.owner || "-"}`} />
      <div className="columns">
        {data.environments.map((env) => (
          <Panel key={env.name} title={env.name}>
            <dl>
              <dt>Repo</dt><dd>{env.git.repo}</dd>
              <dt>Default Branch</dt><dd>{env.branchPolicy.defaultBranch}</dd>
              <dt>Namespace</dt><dd>{env.namespace}</dd>
              <dt>Chart</dt><dd>{env.git.chartPath}</dd>
              <dt>Image</dt><dd>{env.image.repository}</dd>
              <dt>Health</dt><dd>{env.health.healthPath} / {env.health.readyPath}</dd>
            </dl>
          </Panel>
        ))}
      </div>
    </section>
  );
}

function Builds({ navigate }: { navigate: (path: string) => void }) {
  const { data, error, loading } = useAsync(() => api.builds(), []);
  if (loading) return <Loading title="Builds" />;
  if (error) return <ErrorState title="Builds" error={error} />;
  return <Records title="Builds" records={data ?? []} idKey="build_id" navigate={navigate} base="/builds" />;
}

function Deploys({ navigate }: { navigate: (path: string) => void }) {
  const { data, error, loading } = useAsync(() => api.deploys(), []);
  if (loading) return <Loading title="Deploys" />;
  if (error) return <ErrorState title="Deploys" error={error} />;
  return <Records title="Deploys" records={data ?? []} idKey="deploy_id" navigate={navigate} base="/deploys" />;
}

function BuildDetails({ id }: { id: string }) {
  const record = useAsync<BuildRecord>(() => api.build(id), [id]);
  const stages = useAsync<StageRecord[]>(() => api.buildStages(id), [id]);
  return <Details title="Build Details" record={record} stages={stages} />;
}

function DeployDetails({ id }: { id: string }) {
  const record = useAsync<DeployRecord>(() => api.deploy(id), [id]);
  const stages = useAsync<StageRecord[]>(() => api.deployStages(id), [id]);
  return <Details title="Deploy Details" record={record} stages={stages} />;
}

function BuildForm() {
  const [form, setForm] = useState({ service_name: "", branch: "main", commit_sha: "", builder: "operator" });
  const [result, setResult] = useState<string>("");
  const [error, setError] = useState<string>("");
  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setError("");
    try {
      const item = await api.createBuild({ ...form, trigger_type: "manual" });
      setResult(item.build_id);
    } catch (err) {
      setError((err as Error).message);
    }
  }
  return <FormPanel title="Trigger Build" form={form} setForm={setForm} submit={submit} result={result} error={error} />;
}

function DeployForm() {
  const [form, setForm] = useState({ service_name: "", environment: "dev", image_repo: "", image_tag: "", image_digest: "", deployer: "operator" });
  const [dryRunId, setDryRunId] = useState("");
  const [result, setResult] = useState("");
  const [error, setError] = useState("");
  async function dryRun(event: React.FormEvent) {
    event.preventDefault();
    setError("");
    try {
      const item = await api.dryRun(form);
      setDryRunId(item.deploy_id);
    } catch (err) {
      setError((err as Error).message);
    }
  }
  async function confirm() {
    setError("");
    try {
      const item = await api.confirmDeploy({ dry_run_deploy_id: dryRunId, confirmed_by: form.deployer });
      setResult(item.deploy_id);
    } catch (err) {
      setError((err as Error).message);
    }
  }
  return (
    <FormPanel title="Prepare Deploy" form={form} setForm={setForm} submit={dryRun} result={result || dryRunId} error={error}>
      <button className="primary" disabled={!dryRunId} onClick={confirm} type="button">Confirm Deploy</button>
    </FormPanel>
  );
}

function CatalogSettings() {
  return <Services navigate={() => undefined} />;
}

function Details({ title, record, stages }: { title: string; record: ReturnType<typeof useAsync<any>>; stages: ReturnType<typeof useAsync<StageRecord[]>> }) {
  if (record.loading) return <Loading title={title} />;
  if (record.error) return <ErrorState title={title} error={record.error} />;
  if (!record.data) return <Empty title={title} text="Record not found." />;
  return (
    <section>
      <Header title={title} subtitle="Platform fact record and pipeline timeline." />
      <Panel title="Record"><pre>{JSON.stringify(record.data, null, 2)}</pre></Panel>
      <Panel title="Timeline">
        {stages.loading && <p className="muted">Loading stages...</p>}
        {stages.error && <p className="error">{stages.error}</p>}
        {(stages.data ?? []).map((stage) => (
          <div className="stage" key={stage.id}>
            <span className={classForStatus(stage.status)}>{stage.status}</span>
            <strong>{stage.stage}</strong>
            <span>{stage.message || ""}</span>
          </div>
        ))}
        {(stages.data ?? []).length === 0 && !stages.loading && <p className="muted">No stage records.</p>}
      </Panel>
    </section>
  );
}

function FormPanel({ title, form, setForm, submit, result, error, children }: any) {
  const keys = Object.keys(form);
  return (
    <section>
      <Header title={title} subtitle="Actions are submitted through platform APIs only." />
      <form className="form" onSubmit={submit}>
        {keys.map((key) => (
          <label key={key}>
            <span>{key}</span>
            <input value={form[key]} onChange={(event) => setForm({ ...form, [key]: event.target.value })} />
          </label>
        ))}
        <div className="actions">
          <button className="primary" type="submit">{title}</button>
          {children}
        </div>
        {result && <p className="ok">Record: {result}</p>}
        {error && <p className="error">{error}</p>}
      </form>
    </section>
  );
}

function Records({ title, records, idKey, navigate, base }: any) {
  return (
    <section>
      <Header title={title} subtitle="History records from Postgres." />
      {records.length === 0 ? <Empty title={title} text="No records yet." /> : (
        <div className="table">
          <div className="row head"><span>ID</span><span>Service</span><span>Status</span><span>Created</span></div>
          {records.map((record: any) => (
            <button className="row clickable" key={record[idKey]} onClick={() => navigate(`${base}/${record[idKey]}`)}>
              <span>{record[idKey]}</span>
              <span>{record.service_name}</span>
              <span className={classForStatus(record.status)}>{record.status}</span>
              <span>{formatDate(record.created_at)}</span>
            </button>
          ))}
        </div>
      )}
    </section>
  );
}

function RecordList({ title, records, idKey }: any) {
  return (
    <Panel title={title}>
      {records.length === 0 ? <p className="muted">No records.</p> : records.map((record: any) => (
        <div className="compact" key={record[idKey]}>
          <span>{record.service_name}</span>
          <span className={classForStatus(record.status)}>{record.status}</span>
        </div>
      ))}
    </Panel>
  );
}

function Header({ title, subtitle }: { title: string; subtitle: string }) {
  return <header><h1>{title}</h1><p>{subtitle}</p></header>;
}

function Panel({ title, children }: { title: string; children: React.ReactNode }) {
  return <section className="panel"><h2>{title}</h2>{children}</section>;
}

function Metric({ label, value }: { label: string; value: React.ReactNode }) {
  return <div className="metric"><span>{label}</span><strong>{value}</strong></div>;
}

function Loading({ title }: { title: string }) {
  return <Empty title={title} text="Loading..." />;
}

function ErrorState({ title, error }: { title: string; error: string }) {
  return <Empty title={title} text={error} tone="error" />;
}

function Empty({ title, text, tone }: { title: string; text: string; tone?: string }) {
  return <section><Header title={title} subtitle="" /><div className={`empty ${tone ?? ""}`}>{text}</div></section>;
}

function formatDate(value?: string) {
  if (!value) return "-";
  return new Date(value).toLocaleString();
}

createRoot(document.getElementById("root")!).render(<App />);

