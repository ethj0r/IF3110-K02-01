import { cp as Cp, readdir as ReadDir, rm as Rm } from 'node:fs/promises'
import { fileURLToPath as FileURLToPath } from 'node:url'
import { join as Join } from 'node:path'

const Destination = FileURLToPath(new URL('../../backend/internal/webui/dist/', import.meta.url))
const Source = FileURLToPath(new URL('../dist/', import.meta.url))

for (const Entry of await ReadDir(Destination)) {
  if (Entry !== '.gitkeep') {
    await Rm(Join(Destination, Entry), { recursive: true, force: true })
  }
}
await Cp(Source, Destination, { recursive: true })
