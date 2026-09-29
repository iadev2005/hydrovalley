// Copia el código de TypeScript que genera scripts/gen-proto.sh (gen/ts en la
// raíz del repositorio) a src/gen, para que resuelva @bufbuild/protobuf desde
// node_modules de este servicio. src/gen no se versiona.
import { cp, rm, access } from "node:fs/promises";
import { fileURLToPath } from "node:url";

const source = fileURLToPath(new URL("../../../gen/ts/hydrovalley", import.meta.url));
const target = fileURLToPath(new URL("../src/gen/hydrovalley", import.meta.url));

try {
  await access(source);
} catch {
  console.error("gen/ts no existe: ejecuta scripts/gen-proto.sh desde la raíz del repositorio");
  process.exit(1);
}
await rm(target, { recursive: true, force: true });
await cp(source, target, { recursive: true });
console.log(`código generado copiado a ${target}`);
