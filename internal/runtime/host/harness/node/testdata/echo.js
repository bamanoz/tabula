const fs = require('node:fs')

const input = fs.readFileSync(0, 'utf8')
const parsed = input.trim() ? JSON.parse(input) : {}
process.stdout.write(JSON.stringify({ ok: true, input: parsed, platform: 'node' }))
