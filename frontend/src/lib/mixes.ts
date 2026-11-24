import type { PersonalMix } from '@/lib/api/mixes'

/** The small line above a mix title: its genre or where it comes from. */
export function mixLabel(mix: PersonalMix): string {
  switch (mix.kind) {
    case 'genre':
      return mix.genre || 'Genre-Mix'
    case 'on_repeat':
      return 'Meistgehört'
    case 'rediscover':
      return 'Aus deinem Verlauf'
    case 'unheard':
      return 'Entdecken'
    case 'new':
      return 'Neuzugänge'
    default:
      return 'Mix'
  }
}
