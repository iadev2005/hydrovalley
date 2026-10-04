// Panel del sistema: consulta /api/system cada 2 s y pinta la vista. La lógica
// vive en system.js (probada con node:test); aquí solo hay DOM y temporizador.
import { createPollBackoff, loadSystem } from "./system.js";

const $ = (id) => document.getElementById(id);

const backoff = createPollBackoff();
let timer = null;
let run = 0; // invalida la respuesta de una consulta anterior a una pausa

function renderServices(services) {
  $("services-list").replaceChildren(
    ...services.map((service) => {
      const item = document.createElement("li");
      item.className = service.up ? "service up" : "service down";
      const dot = document.createElement("span");
      dot.className = "dot";
      dot.setAttribute("aria-hidden", "true");
      const text = document.createElement("span");
      text.textContent = `${service.label}: ${service.statusText}`;
      item.append(dot, text);
      if (service.detail) {
        const detail = document.createElement("span");
        detail.className = "detail";
        detail.textContent = ` (${service.detail})`;
        item.append(detail);
      }
      return item;
    }),
  );
}

function renderHub(hub) {
  const hasHub = hub !== null;
  $("hub-section").hidden = !hasHub;
  $("valley-section").hidden = !hasHub;
  $("hub-missing").hidden = hasHub;
  if (!hasHub) return;
  for (const el of document.querySelectorAll("[data-field]")) {
    el.textContent = hub[el.dataset.field] ?? "—";
  }
}

function render(result) {
  $("system-error").hidden = result.ok;
  if (!result.ok) {
    $("system-error").textContent = result.error.message;
    return;
  }
  const { view } = result;
  renderServices(view.services);
  renderHub(view.hub);
  const when = new Date(view.generatedAt);
  $("system-updated").textContent = Number.isNaN(when.getTime())
    ? "Última actualización: —"
    : `Última actualización: ${when.toLocaleTimeString("es-VE")}`;
}

async function poll() {
  clearTimeout(timer);
  timer = null;
  const current = (run += 1);
  let wait;
  try {
    const result = await loadSystem(window.fetch.bind(window));
    if (current !== run) return; // la pestaña se ocultó mientras esperaba
    render(result);
    wait = backoff.next(result);
  } catch (error) {
    // Un fallo inesperado no debe detener el refresco.
    console.error("no se pudo actualizar el panel del sistema", error);
    wait = backoff.next({ ok: false, error: { kind: "offline", waitMs: 0 } });
  }
  if (current !== run || document.hidden) return;
  timer = setTimeout(poll, wait);
}

// Con la pestaña oculta no se consulta: ahorra cupo del límite por IP.
document.addEventListener("visibilitychange", () => {
  if (document.hidden) {
    clearTimeout(timer);
    timer = null;
    run += 1;
  } else if (timer === null) {
    poll();
  }
});

poll();
