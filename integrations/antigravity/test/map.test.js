import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  agyModeToPersona,
  cleanGenericOutput,
  cleanUserText,
  mapToolInput,
  mapToolName,
  personaModeToAgy,
  stepsToItems,
  toAgyId,
  toPersonaId,
} from '../lib/map.js'

describe('antigravity transcript and tool mapping', () => {
  it('converts session ids between persona and agy', () => {
    assert.equal(toPersonaId('ad40e999-0253-4097-8ee2-a70f9259e88b'), 'agy-ad40e999-0253-4097-8ee2-a70f9259e88b')
    assert.equal(toPersonaId('agy-ad40e999-0253-4097-8ee2-a70f9259e88b'), 'agy-ad40e999-0253-4097-8ee2-a70f9259e88b')
    assert.equal(toAgyId('agy-ad40e999-0253-4097-8ee2-a70f9259e88b'), 'ad40e999-0253-4097-8ee2-a70f9259e88b')
  })

  it('extracts user request and strips persona-context', () => {
    const raw = `<USER_REQUEST>\n<persona-context>\nsnapshot\n</persona-context>\n\nHello world\n</USER_REQUEST>\n<ADDITIONAL_METADATA>\ntime\n</ADDITIONAL_METADATA>`
    assert.equal(cleanUserText(raw), 'Hello world')
  })

  it('maps tool names and inputs to Persona conventions', () => {
    assert.equal(mapToolName('run_command'), 'Bash')
    assert.deepEqual(mapToolInput('run_command', { CommandLine: 'npm test', toolSummary: 'Run tests' }), {
      command: 'npm test',
      description: 'Run tests',
    })
    assert.equal(mapToolName('view_file'), 'Read')
    assert.deepEqual(mapToolInput('view_file', { AbsolutePath: '/tmp/a.txt' }), { file_path: '/tmp/a.txt' })
    assert.equal(mapToolName('replace_file_content'), 'Edit')
    assert.deepEqual(
      mapToolInput('replace_file_content', {
        TargetFile: '/tmp/a.txt',
        TargetContent: 'old',
        ReplacementContent: 'new',
      }),
      { file_path: '/tmp/a.txt', old_string: 'old', new_string: 'new' },
    )
  })

  it('maps transcript steps into ordered Persona items with paired tool results', () => {
    const steps = [
      {
        step_index: 0,
        source: 'USER_EXPLICIT',
        type: 'USER_INPUT',
        status: 'DONE',
        created_at: '2026-11-05T10:00:00Z',
        content: '<USER_REQUEST>\nCheck files\n</USER_REQUEST>',
      },
      {
        step_index: 1,
        source: 'MODEL',
        type: 'PLANNER_RESPONSE',
        status: 'DONE',
        created_at: '2026-11-05T10:00:01Z',
        thinking: 'Listing files first',
        content: 'I will list the directory.',
        tool_calls: [{ name: 'run_command', args: { CommandLine: 'ls -la', toolSummary: 'List files' } }],
      },
      {
        step_index: 2,
        source: 'MODEL',
        type: 'GENERIC',
        status: 'DONE',
        created_at: '2026-11-05T10:00:02Z',
        content: 'Created At: 2026-11-05T10:00:02Z\nOutput:\ntotal 0\n',
      },
    ]

    const { items, toSeq, lastText } = stepsToItems(steps)
    assert.equal(toSeq, 3)
    assert.equal(lastText, 'I will list the directory.')
    assert.equal(items.length, 5)
    assert.equal(items[0].kind, 'text')
    assert.equal(items[0].role, 'user')
    assert.equal(items[0].text, 'Check files')
    assert.equal(items[1].kind, 'thinking')
    assert.equal(items[2].kind, 'text')
    assert.equal(items[3].kind, 'tool_use')
    assert.equal(items[3].tool_name, 'Bash')
    assert.equal(items[4].kind, 'tool_result')
    assert.equal(items[4].tool_use_id, items[3].tool_use_id)
    assert.equal(items[4].text.trim(), 'total 0')
  })

  it('maps modes between Persona and Antigravity ACP', () => {
    assert.equal(personaModeToAgy('plan'), 'plan')
    assert.equal(personaModeToAgy('acceptEdits'), 'accept-edits')
    assert.equal(personaModeToAgy('auto'), 'auto')
    assert.equal(agyModeToPersona('auto'), 'auto')
    assert.equal(personaModeToAgy('default'), 'default')
    assert.equal(agyModeToPersona('accept-edits'), 'acceptEdits')
    assert.equal(cleanGenericOutput('Created At: 2026\nOutput:\nhello'), 'hello')
  })
})
