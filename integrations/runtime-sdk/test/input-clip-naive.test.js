import { test } from 'node:test'
import assert from 'node:assert/strict'
import { boundInput } from '../lib/input-clip.js'

const clipNoteFormat = '\n…（已截断，共 %d 字节）'
const jsonBytes = (value) => Buffer.byteLength(JSON.stringify(value), 'utf8')
const keyCompare = (a, b) => Buffer.compare(Buffer.from(a, 'utf8'), Buffer.from(b, 'utf8'))

// Deliberately independent of InputClipTree and its heaps: walk only attached values and re-encode before every removal.
function naiveBoundInput(candidate, originalSize, budget) {
  const value = JSON.parse(JSON.stringify(candidate))
  let markerKey = 'zz_persona_clipped'
  for (let suffix = 2; Object.hasOwn(value, markerKey); suffix++) markerKey = `zz_persona_clipped_${suffix}`
  let arrayItems = 0
  let objectFields = 0
  const withMarker = () => ({
    ...value,
    [markerKey]: { note: `Omitted ${arrayItems} array items and ${objectFields} object fields.${clipNoteFormat.replace('%d', String(originalSize))}` },
  })
  for (let steps = 0; steps < 100_000; steps++) {
    if (jsonBytes(withMarker()) <= budget) return withMarker()
    const arrays = []
    const objects = []
    function walk(node) {
      if (Array.isArray(node)) {
        arrays.push(node)
        for (const entry of node) walk(entry)
      } else if (node !== null && typeof node === 'object') {
        objects.push(node)
        for (const key of Object.keys(node).sort(keyCompare)) walk(node[key])
      }
    }
    walk(value)
    const best = (nodes, length) => nodes.reduce((selected, node) => (
      length(node) > 0 && (selected === null || length(node) > length(selected)) ? node : selected
    ), null)
    const array = best(arrays, (node) => node.length)
    if (array !== null) {
      array.pop()
      arrayItems++
      continue
    }
    const object = best(objects, (node) => Object.keys(node).length)
    if (object === null) return withMarker()
    const keys = Object.keys(object).sort(keyCompare)
    delete object[keys[keys.length - 1]]
    objectFields++
  }
  assert.fail('naive reference exceeded its removal limit')
}

function checkNaive(candidate, budget) {
  const originalSize = jsonBytes(candidate) + 7 // exercise an original wire size distinct from the candidate's
  const got = boundInput(candidate, originalSize, budget, clipNoteFormat)
  const want = naiveBoundInput(candidate, originalSize, budget)
  assert.deepEqual(got, want)
  assert.ok(jsonBytes(got) <= budget, `output is ${jsonBytes(got)} bytes, budget is ${budget}`)
  assert.deepEqual(candidate, JSON.parse(JSON.stringify(candidate)), 'candidate is not JSON-normalizable')
}

test('boundInput matches an independent naive reference across boundary budgets', () => {
  const wide = Object.fromEntries(Array.from({ length: 80 }, (_, i) => [`k${String(i).padStart(3, '0')}`, i]))
  const candidates = [
    { values: Array.from({ length: 50 }, () => '1234567') },
    { a: { small: [1, 2, 3], wide } },
    { b: Array.from({ length: 30 }, (_, i) => [String(i), 'a'.repeat(17)]), a: Array.from({ length: 30 }, (_, i) => [String(i), 'b'.repeat(18)]) },
    { rows: { [`c${'k'.repeat(30)}`]: 'x'.repeat(20), [`𐀀${'k'.repeat(30)}`]: 'x'.repeat(20), [`${'k'.repeat(30)}`]: 'x'.repeat(20) } },
    { zz_persona_clipped: 'user value', values: Array.from({ length: 50 }, () => '1234567') },
  ]
  for (const candidate of candidates) {
    for (let budget = 256; budget <= jsonBytes(candidate) + 160; budget++) checkNaive(candidate, budget)
  }
})

function seededRandom(seed) {
  let state = seed >>> 0
  return (limit) => {
    state ^= state << 13
    state ^= state >>> 17
    state ^= state << 5
    return (state >>> 0) % limit
  }
}

function randomValue(random, depth) {
  if (depth === 0 || random(3) === 0) {
    return ['x'.repeat(random(30)), '<>&  ', random(100), random(2) === 0, null][random(5)]
  }
  if (random(2) === 0) return Array.from({ length: random(5) }, () => randomValue(random, depth - 1))
  const out = {}
  const keys = ['k3', 'k0', '10', '2', '', '𐀀', 'title', 'url']
  for (let i = 0; i < random(5); i++) out[`${keys[random(keys.length)]}_${i}`] = randomValue(random, depth - 1)
  return out
}

test('boundInput matches the naive reference on seeded unsorted and Unicode objects', () => {
  const random = seededRandom(2907)
  for (let i = 0; i < 80; i++) {
    const root = {}
    for (let j = 5; j >= 0; j--) root[`field_${j}`] = randomValue(random, 3)
    root[String((i * 731) % 2000)] = randomValue(random, 3)
    root[i % 2 === 0 ? '𐀀' : ''] = randomValue(random, 3)
    root.values = randomValue(random, 3)
    for (const budget of [256, 512, 1024]) checkNaive(root, budget)
  }
})
