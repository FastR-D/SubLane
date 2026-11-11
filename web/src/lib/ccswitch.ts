import { z } from 'zod'

export const ccSwitchApps = [
  'codex',
  'opencode',
  'claude',
  'openclaw',
  'hermes',
  'gemini',
  'grokbuild',
] as const
const appSchema = z.enum(ccSwitchApps)
export type ImportApp = z.infer<typeof appSchema>

function isOrigin(value: string) {
  try {
    const url = new URL(value)
    return (
      ['http:', 'https:'].includes(url.protocol) &&
      !url.username &&
      !url.password &&
      !url.search &&
      !url.hash &&
      url.pathname === '/'
    )
  } catch {
    return false
  }
}
const settingsSchema = z.object({
  app: appSchema.default('codex'),
  origin: z
    .string()
    .refine(isOrigin)
    .transform((value) => new URL(value).origin),
  name: z
    .string()
    .trim()
    .min(1)
    .max(256)
    .refine(
      (value) =>
        Array.from(value).length <= 128 &&
        !Array.from(value).some((character) => {
          const code = character.charCodeAt(0)
          return code < 32 || (code >= 127 && code <= 159)
        }),
    ),
  model: z
    .string()
    .trim()
    .regex(/^[a-zA-Z0-9][a-zA-Z0-9._:/()+-]{0,159}$/)
    .refine(
      (value) =>
        value.replace(/^(codex|claude|antigravity)\//, '').length <= 128,
    ),
})
const secretSchema = z.string().regex(/^sl_[A-Za-z0-9_-]{43}$/)
export type ImportSettings = z.input<typeof settingsSchema>

export function validImportSettings(input: ImportSettings) {
  return settingsSchema.safeParse(input).success
}

export function ccSwitchEndpoint(origin: string, app: ImportApp) {
  // Native clients append /v1/messages or /v1beta themselves; OpenAI clients need the /v1 base.
  return app === 'claude' || app === 'gemini' ? origin : `${origin}/v1`
}

export function ccSwitchLink(input: ImportSettings & { secret: string }) {
  const settings = settingsSchema.parse(input)
  const secret = secretSchema.parse(input.secret)
  const params = new URLSearchParams({
    resource: 'provider',
    app: settings.app,
    name: settings.name,
    endpoint: ccSwitchEndpoint(settings.origin, settings.app),
    apiKey: secret,
    model: settings.model,
    homepage: settings.origin,
    enabled: 'false',
  })
  if (settings.app === 'claude') {
    // Keep Claude's built-in aliases inside this key's allowed model catalog.
    for (const alias of ['haikuModel', 'sonnetModel', 'opusModel']) {
      params.set(alias, settings.model)
    }
  }
  return `ccswitch://v1/import?${params.toString()}`
}

export function openCCSwitch(link: string) {
  const url = new URL(link)
  if (
    url.protocol !== 'ccswitch:' ||
    url.host !== 'v1' ||
    url.pathname !== '/import' ||
    url.searchParams.get('resource') !== 'provider' ||
    !appSchema.safeParse(url.searchParams.get('app')).success
  ) {
    throw new Error('invalid_cc_switch_link')
  }
  // Use only the local app protocol. Never send a credential-bearing link to a web relay or history API.
  window.location.assign(link)
}

// Built-in Codex image models are routed only through /v1/images.
export function isImageModel(id: string) {
  return /^(?:[a-z]+\/)?gpt-image-/i.test(id.trim())
}
