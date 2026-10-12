// Icons outside the kit's set (@go-tangra/ui ICONS) that the pages use. The
// shell's stylesheet carries only the kit set and Tailwind emits only the class
// names it finds in the sources, so this remote's stylesheet carries these.
// Navigation icons must be in the kit set (the shell draws the menu):
// pkg/asteriskmanifest TestNavIconsInKitSet.
export const EXTRA_ICONS = [
  'icon-[mdi--account-voice]',
  'icon-[mdi--headset]',
  'icon-[mdi--phone-cancel]',
  'icon-[mdi--phone-check]',
  'icon-[mdi--phone-forward]',
  'icon-[mdi--phone-in-talk]',
  'icon-[mdi--phone-incoming]',
  'icon-[mdi--phone-missed]',
  'icon-[mdi--phone-off]',
  'icon-[mdi--phone-outgoing]',
  'icon-[mdi--phone-paused]',
  'icon-[mdi--phone-remove]',
  'icon-[mdi--phone-return]',
  'icon-[mdi--phone-ring]',
  'icon-[mdi--signal]',
  'icon-[mdi--timer-outline]',
] as const
