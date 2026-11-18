import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { clipInput, MAX_INPUT } from '../index.js'

const shared = JSON.parse(readFileSync(new URL('../../../spec/fixtures/input-clip/cases.json', import.meta.url), 'utf8'))
const jsonBytes = (value) => Buffer.byteLength(JSON.stringify(value), 'utf8')

function expand(value, index = 0) {
  if (Array.isArray(value)) return value.map((entry) => expand(entry, index))
  if (value === null || typeof value !== 'object') return value
  if (Object.hasOwn(value, '$pad_object_to')) {
    const out = expand(value.value, index)
    assert.equal(out.pad, undefined)
    out.pad = ''
    const padding = value.$pad_object_to - jsonBytes(out)
    assert.ok(padding >= 0, 'padded object is already over its target')
    out.pad = 'x'.repeat(padding)
    assert.equal(jsonBytes(out), value.$pad_object_to)
    return out
  }
  if (Object.hasOwn(value, '$keyed_fields')) {
    return Object.fromEntries(value.$keyed_fields.map((key, i) => [
      key + value.suffix.repeat(value.count), expand(value.value, i),
    ]))
  }
  if (Object.hasOwn(value, '$repeat')) {
    return Array.from({ length: value.$repeat }, (_, i) => expand(value.value, i))
  }
  if (Object.hasOwn(value, '$single_field')) {
    return { [expand(value.$single_field, index)]: expand(value.value, index) }
  }
  if (Object.hasOwn(value, '$fields')) {
    const base = Object.fromEntries(Object.entries(value.base ?? {}).map(([key, entry]) => [key, expand(entry, index)]))
    const fields = Object.fromEntries(Array.from({ length: value.$fields }, (_, i) => [
      `${value.key}${String(i).padStart(value.pad, '0')}`, expand(value.value, i),
    ]))
    return { ...base, ...fields }
  }
  if (Object.hasOwn(value, '$index')) return String(index).padStart(value.$index, '0')
  if (Object.hasOwn(value, '$string')) return value.$string.repeat(value.count)
  return Object.fromEntries(Object.entries(value).map(([key, entry]) => [key, expand(entry, index)]))
}

function markerCounts(note) {
  const match = note.match(/^Omitted (\d+) array items and (\d+) object fields\./)
  assert.ok(match, `bad marker: ${note}`)
  return { arrayItems: Number(match[1]), objectFields: Number(match[2]) }
}

for (const c of shared.cases) {
  test(`clipInput shared case: ${c.name}`, () => {
    const input = expand(c.input)
    const original = structuredClone(input)
    const inputBytes = jsonBytes(input)
    const expectedInputBytes = c.expected.sdk_input_bytes ?? c.expected.input_bytes
    if (expectedInputBytes) assert.equal(inputBytes, expectedInputBytes)
    const out = clipInput(input)
    assert.deepEqual(input, original, 'the source input was mutated')
    assert.deepEqual(clipInput(input), out, 'clipping is not deterministic')
    const outputBytes = jsonBytes(out)
    assert.ok(outputBytes <= MAX_INPUT, `output is ${outputBytes} bytes`)
    assert.equal(clipInput(out), out, 'a bounded result did not return unchanged')
    const expectedBytes = c.expected.sdk_output_bytes ?? c.expected.output_bytes
    if (expectedBytes) assert.equal(outputBytes, expectedBytes)
    if (!c.expected.marker) {
      assert.equal(out, input, 'an at-budget input did not return the original object')
      return
    }
    const markerKey = c.expected.marker_key ?? 'zz_persona_clipped'
    const markerValue = out[markerKey]
    assert.deepEqual(Object.keys(markerValue), ['note'])
    const note = markerValue.note
    assert.equal(typeof note, 'string')
    assert.ok(note.endsWith(`\n…（已截断，共 ${inputBytes} 字节）`), note)
    const { arrayItems, objectFields } = markerCounts(note)
    const expectedObjectFields = c.expected.sdk_object_fields ?? c.expected.object_fields
    const remaining = c.expected.sdk_remaining ?? c.expected.remaining
    if (c.expected.array_items !== undefined) assert.equal(arrayItems, c.expected.array_items)
    if (expectedObjectFields !== undefined) assert.equal(objectFields, expectedObjectFields)

    switch (c.expected.check) {
      case 'short_values': {
        assert.equal(out.values.length, remaining)
        for (const [i, row] of out.values.entries()) {
          assert.deepEqual(row, [String(i), 'a'.repeat(17), 'b'.repeat(18)])
        }
        assert.equal(arrayItems, 4000 - out.values.length)
        assert.equal(objectFields, 0)
        break
      }
      case 'nested_arrays': {
        assert.equal(out.outer.length, remaining)
        assert.deepEqual(c.expected.inner, [78])
        for (const row of out.outer) {
          assert.ok(Array.isArray(row))
          assert.equal(row.length, c.expected.inner[0])
          for (const value of row) assert.equal(value, 'x')
        }
        assert.equal(objectFields, 0)
        break
      }
      case 'object_array': {
        assert.equal(out.file_path, '/home/user/project/a.go')
        assert.equal(out.edits.length, remaining)
        for (const edit of out.edits) assert.deepEqual(edit, { old_string: 'a', new_string: 'b' })
        break
      }
      case 'todo_array': {
        assert.equal(out.todos.length, remaining)
        for (const todo of out.todos) assert.deepEqual(todo, { content: 'Do the work', activeForm: 'Doing the work', status: 'pending' })
        break
      }
      case 'question_array': {
        assert.equal(out.questions.length, remaining)
        for (const question of out.questions) {
          assert.deepEqual(question, {
            question: 'Choose one?', header: 'Choice', options: [{ label: 'A', description: 'First' }], multi_select: false,
          })
        }
        break
      }
      case 'wide_object': {
        assert.equal(out.values.length, 0)
        assert.equal(arrayItems, 100)
        const fields = Object.keys(out.wide)
        assert.equal(fields.length, remaining)
        assert.equal(objectFields, 2000 - fields.length)
        for (const [i, key] of fields.entries()) {
          assert.equal(key, `a_long_field_name_with_many_bytes_${String(i).padStart(4, '0')}`)
          assert.equal(out.wide[key], String(i))
        }
        break
      }
      case 'root_wide': {
        assert.equal(arrayItems, 0)
        const fields = Object.keys(out).filter((key) => key !== markerKey)
        assert.equal(fields.length, remaining)
        assert.equal(objectFields, 2000 - fields.length)
        for (const [i, key] of fields.entries()) {
          assert.equal(key, `${c.expected.key_prefix}${String(i).padStart(4, '0')}`)
          assert.equal(out[key], c.expected.value ?? String(i))
        }
        break
      }
      case 'single_long_key': {
        assert.deepEqual(Object.keys(out), [markerKey])
        assert.equal(arrayItems, 0)
        assert.equal(objectFields, 1)
        break
      }
      case 'mixed_strings': {
        assert.ok(out.long.startsWith('abcdef'))
        assert.ok(out.long.endsWith(`\n…（已截断，共 6000 字节）`))
        assert.equal(out.values.length, remaining)
        for (const [i, row] of out.values.entries()) assert.deepEqual(row, ['short', String(i), true, null])
        break
      }
      case 'one_byte_over': {
        assert.equal(out.values.length, remaining)
        assert.equal(arrayItems, 2430 - out.values.length)
        assert.equal(objectFields, 0)
        for (const value of out.values) assert.equal(value, '1234567')
        assert.equal(out.pad, input.pad)
        // One more retained item, with the correspondingly shorter count in the marker, must not fit.
        const restored = { ...out, values: [...out.values, '1234567'], [markerKey]: { note: `Omitted ${arrayItems - 1} array items and ${objectFields} object fields.\n…（已截断，共 ${inputBytes} 字节）` } }
        assert.ok(jsonBytes(restored) > MAX_INPUT, 'the boundary cut is not minimal')
        break
      }
      case 'nested_empty': {
        assert.deepEqual(out.a.small, [])
        assert.equal(Object.keys(out.a.wide).length, remaining)
        for (let i = 0; i < remaining; i++) assert.equal(out.a.wide[`k${String(i).padStart(4, '0')}`], 1)
        const restored = {
          ...out,
          a: {
            ...out.a,
            wide: { ...out.a.wide, [`k${String(remaining).padStart(4, '0')}`]: 1 },
          },
          [markerKey]: { note: `Omitted ${arrayItems} array items and ${objectFields - 1} object fields.\n…（已截断，共 ${inputBytes} 字节）` },
        }
        assert.ok(jsonBytes(restored) > MAX_INPUT, 'one more nested field would still fit')
        break
      }
      case 'unsorted_root': {
        const fields = Object.keys(out).filter((key) => key !== markerKey)
        assert.equal(fields.length, remaining)
        assert.equal(arrayItems, 0)
        for (const key of c.expected.absent) assert.ok(!Object.hasOwn(out, key), `retained ${key}`)
        const generated = c.expected.total - (objectFields - c.expected.absent.length)
        assert.equal(generated, fields.length)
        for (let i = 0; i < generated; i++) {
          const key = `${c.expected.key_prefix}${String(i).padStart(4, '0')}`
          assert.equal(out[key], String(i))
        }
        break
      }
      case 'two_arrays': {
        for (const [name, expectedLength] of [['a', c.expected.a_remaining], ['b', c.expected.b_remaining]]) {
          assert.equal(out[name].length, expectedLength)
          for (const [i, row] of out[name].entries()) assert.deepEqual(row, [String(i), 'a'.repeat(17), 'b'.repeat(18)])
        }
        break
      }
      case 'integer_keys': {
        const fields = Object.keys(out.rows)
        assert.equal(fields.length, remaining)
        assert.equal(arrayItems, 0)
        const expectedKeys = Array.from({ length: 4000 }, (_, i) => String(i)).sort((a, b) => Buffer.compare(Buffer.from(a, 'utf8'), Buffer.from(b, 'utf8')))
        assert.equal(expectedKeys[remaining - 1], c.expected.last_key)
        for (const key of expectedKeys.slice(0, remaining)) assert.equal(out.rows[key], key)
        break
      }
      case 'unicode_keys': {
        assert.equal(Object.keys(out.rows).length, remaining)
        assert.equal(arrayItems, 0)
        for (const [prefixes, present] of [[c.expected.present, true], [c.expected.absent, false]]) {
          for (const prefix of prefixes) {
            const key = prefix + 'k'.repeat(8000)
            assert.ok(Object.hasOwn(input.rows, key))
            assert.equal(Object.hasOwn(out.rows, key), present, `Unicode key prefix ${prefix}`)
          }
        }
        break
      }
      case 'marker_collision_base': {
        assert.equal(out.zz_persona_clipped, 'keep this user value')
        assert.equal(markerKey, 'zz_persona_clipped_2')
        assert.equal(out.values.length, remaining)
        assert.equal(arrayItems, 4000 - out.values.length)
        break
      }
      case 'marker_collision': {
        assert.equal(out.zz_persona_clipped, 'keep this user value')
        assert.equal(out.zz_persona_clipped_2, 'keep this second user value')
        assert.equal(out.values.length, remaining)
        assert.equal(arrayItems, 4000 - out.values.length)
        break
      }
      default:
        assert.fail(`unknown shared check: ${c.expected.check}`)
    }
  })
}
