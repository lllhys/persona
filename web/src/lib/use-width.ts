// An element's width in pixels, measured before the first paint and again on every resize; 0 until it is laid out. The
// charts draw at the real pixel size with it (MetricChart, UsageSection, ExtensionChart).
import { useLayoutEffect, useRef, useState } from "react";

export function useWidth<T extends HTMLElement>(): [React.RefObject<T | null>, number] {
  const ref = useRef<T>(null);
  const [w, setW] = useState(0);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    setW(el.clientWidth);
    const ro = new ResizeObserver(([e]) => setW(Math.round(e.contentRect.width)));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  return [ref, w];
}
