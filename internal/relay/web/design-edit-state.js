'use strict';
(() => {
  const transition = value => ({kind:value?.kind || 'cut', duration_seconds:value?.kind === 'crossfade' ? value.duration_seconds : 0});
  // Compare editable content, not server metadata or defaults materialized by forms.
  function contentKey(design) {
    const {id, version, updated_at, ...content} = design;
    content.duration_mode ||= 'fixed';
    content.loop_transition = transition(content.loop_transition);
    content.ending_fade_seconds ??= content.stage === 'ending' ? 1 : null;
    content.soundtrack = content.soundtrack ? {
      ...content.soundtrack, asset:content.soundtrack.playlist ? null : content.soundtrack.asset,
      playlist:content.soundtrack.playlist || null, mode:content.soundtrack.mode || 'end',
      volume_percent:content.soundtrack.volume_percent ?? 100,
      fade_in_seconds:content.soundtrack.fade_in_seconds || 0, fade_out_seconds:content.soundtrack.fade_out_seconds || 0
    } : null;
    content.scenes = content.scenes.map(scene => ({
      ...scene, font:scene.font || '', font_size:scene.font_size || 0, alignment:scene.alignment || '',
      media_kind:scene.media_kind || 'image', items:scene.items || [], image:scene.image || null,
      video:scene.video ? {...scene.video, trim_start_seconds:scene.video.trim_start_seconds || 0,
        trim_end_seconds:scene.video.trim_end_seconds || 0, repeat:!!scene.video.repeat,
        audio_enabled:!!scene.video.audio_enabled, audio_volume_percent:scene.video.audio_volume_percent ?? 100} : null,
      transition:transition(scene.transition),
      content_region:{width_percent:scene.content_region?.width_percent || 0, height_percent:scene.content_region?.height_percent || 0},
      animation_loops:scene.animation_loops ?? null,
      duration_seconds:scene.animation_loops != null ? null : scene.duration_seconds
    }));
    const ordered = value => Array.isArray(value) ? value.map(ordered) : value && typeof value === 'object' ? Object.fromEntries(Object.keys(value).sort().map(key=>[key,ordered(value[key])])) : value;
    return JSON.stringify(ordered(content));
  }
  window.designEditState = {contentKey};
})();
