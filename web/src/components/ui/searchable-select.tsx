import { useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Check, ChevronDown, Search } from "lucide-react";
import { cn } from "@/lib/utils";

export interface SearchableSelectOption {
  value: string;
  label: string;
}

export function SearchableSelect({ id, label, value, options, onChange, onManualSelect, placeholder, searchPlaceholder = "输入关键字搜索…", allowManual = true, disabled = false }: {
  id: string;
  label: string;
  value: string;
  options: SearchableSelectOption[];
  onChange: (value: string) => void;
  onManualSelect: () => void;
  placeholder: string;
  searchPlaceholder?: string;
  allowManual?: boolean;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const [position, setPosition] = useState<{ left: number; width: number; top?: number; bottom?: number; maxHeight: number }>();
  const triggerRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const listboxId = useId();
  const selected = options.find((option) => option.value === value);
  const normalizedQuery = query.trim().toLocaleLowerCase();
  const filteredOptions = useMemo(() => {
    if (!normalizedQuery) return options;
    return options
      .map((option, index) => {
        const label = option.label.toLocaleLowerCase();
        const optionValue = option.value.toLocaleLowerCase();
        const rank = label === normalizedQuery || optionValue === normalizedQuery
          ? 0
          : label.startsWith(normalizedQuery)
            ? 1
            : optionValue.startsWith(normalizedQuery)
              ? 2
              : `${label} ${optionValue}`.includes(normalizedQuery) ? 3 : -1;
        return { option, index, rank };
      })
      .filter((item) => item.rank >= 0)
      .sort((left, right) => left.rank - right.rank || left.index - right.index)
      .map((item) => item.option);
  }, [normalizedQuery, options]);
  const itemCount = filteredOptions.length + (allowManual ? 1 : 0);

  const updatePosition = () => {
    const trigger = triggerRef.current;
    if (!trigger) return;
    const rect = trigger.getBoundingClientRect();
    const viewportWidth = window.visualViewport?.width ?? window.innerWidth;
    const viewportHeight = window.visualViewport?.height ?? window.innerHeight;
    const margin = 8;
    const gap = 4;
    const width = Math.min(Math.max(rect.width, 240), viewportWidth - margin * 2);
    const left = Math.min(Math.max(rect.left, margin), viewportWidth - width - margin);
    const spaceBelow = viewportHeight - rect.bottom - gap - margin;
    const spaceAbove = rect.top - gap - margin;
    const placeAbove = spaceBelow < 220 && spaceAbove > spaceBelow;
    const available = Math.max(96, placeAbove ? spaceAbove : spaceBelow);
    const maxHeight = Math.min(360, available);
    setPosition(placeAbove
      ? { left, width, bottom: viewportHeight - rect.top + gap, maxHeight }
      : { left, width, top: rect.bottom + gap, maxHeight });
  };

  const close = (restoreFocus = false) => {
    setOpen(false);
    setQuery("");
    if (restoreFocus) requestAnimationFrame(() => triggerRef.current?.focus());
  };

  const selectOption = (index: number) => {
    if (allowManual && index === filteredOptions.length) {
      close(true);
      onManualSelect();
      return;
    }
    const option = filteredOptions[index];
    if (!option) return;
    onChange(option.value);
    close(true);
  };

  useLayoutEffect(() => {
    if (!open) return;
    updatePosition();
    const selectedIndex = options.findIndex((option) => option.value === value);
    const frame = requestAnimationFrame(() => {
      setActiveIndex(selectedIndex >= 0 ? selectedIndex : 0);
      searchRef.current?.focus();
    });
    return () => cancelAnimationFrame(frame);
  }, [open]);

  useEffect(() => {
    if (open) setActiveIndex(0);
  }, [query, open]);

  useEffect(() => {
    if (!open) return;
    const handlePointerDown = (event: PointerEvent) => {
      const target = event.target as Node;
      if (!panelRef.current?.contains(target) && !triggerRef.current?.contains(target)) close();
    };
    const handleViewportChange = (event?: Event) => {
      if (event?.target instanceof Node && panelRef.current?.contains(event.target)) return;
      updatePosition();
    };
    const handleEscapeCapture = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      event.stopPropagation();
      close(true);
    };
    document.addEventListener("pointerdown", handlePointerDown, true);
    window.addEventListener("keydown", handleEscapeCapture, true);
    window.addEventListener("resize", handleViewportChange);
    window.addEventListener("scroll", handleViewportChange, true);
    window.visualViewport?.addEventListener("resize", handleViewportChange);
    window.visualViewport?.addEventListener("scroll", handleViewportChange);
    return () => {
      document.removeEventListener("pointerdown", handlePointerDown, true);
      window.removeEventListener("keydown", handleEscapeCapture, true);
      window.removeEventListener("resize", handleViewportChange);
      window.removeEventListener("scroll", handleViewportChange, true);
      window.visualViewport?.removeEventListener("resize", handleViewportChange);
      window.visualViewport?.removeEventListener("scroll", handleViewportChange);
    };
  }, [open]);

  useEffect(() => {
    if (!open) return;
    document.getElementById(`${listboxId}-option-${activeIndex}`)?.scrollIntoView({ block: "nearest" });
  }, [activeIndex, filteredOptions, listboxId, open]);

  return (
    <>
      <button
        ref={triggerRef}
        id={id}
        type="button"
        role="combobox"
        aria-label={label}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={open ? listboxId : undefined}
        className="flex h-9 w-full min-w-0 items-center justify-between gap-2 rounded-md border border-input bg-card px-3 py-2 text-left text-sm shadow-sm focus:outline-none focus:ring-2 focus:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
        disabled={disabled}
        onClick={() => setOpen((current) => !current)}
        onKeyDown={(event) => {
          if (event.key === "ArrowDown") {
            event.preventDefault();
            setOpen(true);
          }
        }}
      >
        <span className={cn("min-w-0 truncate", selected ? "text-foreground" : "text-muted-foreground")}>{selected?.label ?? placeholder}</span>
        <ChevronDown className="h-4 w-4 shrink-0 opacity-50" />
      </button>
      {open ? createPortal(
        <div ref={panelRef} className="pointer-events-auto fixed z-[70] flex flex-col overflow-hidden rounded-md border bg-popover text-popover-foreground shadow-lg" style={position}>
          <div className="flex shrink-0 items-center gap-2 border-b px-2.5 py-2">
            <Search className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
            <input
              ref={searchRef}
              value={query}
              onChange={(event) => {
                setQuery(event.target.value);
                setActiveIndex(0);
              }}
              className="h-7 min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
              placeholder={searchPlaceholder}
              role="searchbox"
              aria-label={`搜索 ${label}`}
              aria-controls={listboxId}
              aria-activedescendant={itemCount > 0 ? `${listboxId}-option-${activeIndex}` : undefined}
              autoComplete="off"
              onKeyDown={(event) => {
                if (event.key === "Escape") {
                  event.preventDefault();
                  event.stopPropagation();
                  close(true);
                } else if (event.key === "ArrowDown") {
                  event.preventDefault();
                  setActiveIndex((current) => Math.min(current + 1, itemCount - 1));
                } else if (event.key === "ArrowUp") {
                  event.preventDefault();
                  setActiveIndex((current) => Math.max(current - 1, 0));
                } else if (event.key === "Home") {
                  event.preventDefault();
                  setActiveIndex(0);
                } else if (event.key === "End") {
                  event.preventDefault();
                  setActiveIndex(itemCount - 1);
                } else if (event.key === "Enter") {
                  event.preventDefault();
                  selectOption(activeIndex);
                } else if (event.key === "Tab") {
                  close();
                }
              }}
            />
          </div>
          <div id={listboxId} role="listbox" aria-label={`${label}选项列表`} className="min-h-0 flex-1 overflow-y-auto p-1">
            {filteredOptions.map((option, index) => {
              const selectedOption = option.value === value;
              return (
                <button
                  key={option.value || "__empty__"}
                  id={`${listboxId}-option-${index}`}
                  type="button"
                  role="option"
                  aria-selected={selectedOption}
                  className={cn("relative flex w-full min-w-0 items-center rounded-sm py-1.5 pl-8 pr-2 text-left text-sm outline-none", activeIndex === index ? "bg-accent text-accent-foreground" : "hover:bg-accent/70")}
                  onMouseDown={(event) => event.preventDefault()}
                  onMouseEnter={() => setActiveIndex(index)}
                  onClick={() => selectOption(index)}
                >
                  <span className="absolute left-2 flex h-3.5 w-3.5 items-center justify-center">{selectedOption ? <Check className="h-4 w-4" /> : null}</span>
                  <span className="block min-w-0 truncate">{option.label}</span>
                </button>
              );
            })}
            {filteredOptions.length === 0 ? <p className="px-3 py-2 text-sm text-muted-foreground">没有匹配项</p> : null}
            {allowManual ? (
              <button
                id={`${listboxId}-option-${filteredOptions.length}`}
                type="button"
                role="option"
                aria-selected={false}
                className={cn("flex w-full items-center rounded-sm py-1.5 pl-8 pr-2 text-left text-sm outline-none", activeIndex === filteredOptions.length ? "bg-accent text-accent-foreground" : "hover:bg-accent/70")}
                onMouseDown={(event) => event.preventDefault()}
                onMouseEnter={() => setActiveIndex(filteredOptions.length)}
                onClick={() => selectOption(filteredOptions.length)}
              >
                手动输入
              </button>
            ) : null}
          </div>
        </div>,
        document.body,
      ) : null}
    </>
  );
}

export function SearchableMultiSelect({ id, label, values, options, onChange, onManualSelect, placeholder, searchPlaceholder = "输入关键字搜索…", disabled = false }: {
  id: string;
  label: string;
  values: string[];
  options: SearchableSelectOption[];
  onChange: (values: string[]) => void;
  onManualSelect?: () => void;
  placeholder: string;
  searchPlaceholder?: string;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const [position, setPosition] = useState<{ left: number; width: number; top?: number; bottom?: number; maxHeight: number }>();
  const triggerRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const listboxId = useId();
  const normalizedQuery = query.trim().toLocaleLowerCase();
  const filteredOptions = useMemo(() => {
    if (!normalizedQuery) return options;
    return options
      .map((option, index) => {
        const optionLabel = option.label.toLocaleLowerCase();
        const optionValue = option.value.toLocaleLowerCase();
        const rank = optionLabel === normalizedQuery || optionValue === normalizedQuery
          ? 0
          : optionLabel.startsWith(normalizedQuery)
            ? 1
            : optionValue.startsWith(normalizedQuery)
              ? 2
              : `${optionLabel} ${optionValue}`.includes(normalizedQuery) ? 3 : -1;
        return { option, index, rank };
      })
      .filter((item) => item.rank >= 0)
      .sort((left, right) => left.rank - right.rank || left.index - right.index)
      .map((item) => item.option);
  }, [normalizedQuery, options]);
  const itemCount = filteredOptions.length + (onManualSelect ? 1 : 0);
  const selectedOptions = values.map((value) => options.find((option) => option.value === value)).filter((option): option is SearchableSelectOption => Boolean(option));
  const triggerText = selectedOptions.length === 0
    ? placeholder
    : selectedOptions.length <= 2
      ? selectedOptions.map((option) => option.label).join("、")
      : `已选择 ${selectedOptions.length} 项`;

  const updatePosition = () => {
    const trigger = triggerRef.current;
    if (!trigger) return;
    const rect = trigger.getBoundingClientRect();
    const viewportWidth = window.visualViewport?.width ?? window.innerWidth;
    const viewportHeight = window.visualViewport?.height ?? window.innerHeight;
    const margin = 8;
    const gap = 4;
    const width = Math.min(Math.max(rect.width, 260), viewportWidth - margin * 2);
    const left = Math.min(Math.max(rect.left, margin), viewportWidth - width - margin);
    const spaceBelow = viewportHeight - rect.bottom - gap - margin;
    const spaceAbove = rect.top - gap - margin;
    const placeAbove = spaceBelow < 240 && spaceAbove > spaceBelow;
    const available = Math.max(112, placeAbove ? spaceAbove : spaceBelow);
    const maxHeight = Math.min(380, available);
    setPosition(placeAbove
      ? { left, width, bottom: viewportHeight - rect.top + gap, maxHeight }
      : { left, width, top: rect.bottom + gap, maxHeight });
  };

  const close = (restoreFocus = false) => {
    setOpen(false);
    setQuery("");
    if (restoreFocus) requestAnimationFrame(() => triggerRef.current?.focus());
  };

  const toggleOption = (index: number) => {
    if (onManualSelect && index === filteredOptions.length) {
      close(true);
      onManualSelect();
      return;
    }
    const option = filteredOptions[index];
    if (!option) return;
    onChange(values.includes(option.value)
      ? values.filter((value) => value !== option.value)
      : [...values, option.value]);
  };

  const selectAllFiltered = () => {
    const next = new Set(values);
    for (const option of filteredOptions) next.add(option.value);
    onChange([...next]);
  };

  useLayoutEffect(() => {
    if (!open) return;
    updatePosition();
    const firstSelected = filteredOptions.findIndex((option) => values.includes(option.value));
    const frame = requestAnimationFrame(() => {
      setActiveIndex(firstSelected >= 0 ? firstSelected : 0);
      searchRef.current?.focus();
    });
    return () => cancelAnimationFrame(frame);
  }, [open]);

  useEffect(() => {
    if (open) setActiveIndex(0);
  }, [query, open]);

  useEffect(() => {
    if (!open) return;
    const handlePointerDown = (event: PointerEvent) => {
      const target = event.target as Node;
      if (!panelRef.current?.contains(target) && !triggerRef.current?.contains(target)) close();
    };
    const handleViewportChange = (event?: Event) => {
      if (event?.target instanceof Node && panelRef.current?.contains(event.target)) return;
      updatePosition();
    };
    const handleEscapeCapture = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      event.stopPropagation();
      close(true);
    };
    document.addEventListener("pointerdown", handlePointerDown, true);
    window.addEventListener("keydown", handleEscapeCapture, true);
    window.addEventListener("resize", handleViewportChange);
    window.addEventListener("scroll", handleViewportChange, true);
    window.visualViewport?.addEventListener("resize", handleViewportChange);
    window.visualViewport?.addEventListener("scroll", handleViewportChange);
    return () => {
      document.removeEventListener("pointerdown", handlePointerDown, true);
      window.removeEventListener("keydown", handleEscapeCapture, true);
      window.removeEventListener("resize", handleViewportChange);
      window.removeEventListener("scroll", handleViewportChange, true);
      window.visualViewport?.removeEventListener("resize", handleViewportChange);
      window.visualViewport?.removeEventListener("scroll", handleViewportChange);
    };
  }, [open]);

  useEffect(() => {
    if (!open) return;
    document.getElementById(`${listboxId}-option-${activeIndex}`)?.scrollIntoView({ block: "nearest" });
  }, [activeIndex, filteredOptions, listboxId, open]);

  return (
    <>
      <button
        ref={triggerRef}
        id={id}
        type="button"
        role="combobox"
        aria-label={label}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={open ? listboxId : undefined}
        title={triggerText}
        className="flex h-9 w-full min-w-0 items-center justify-between gap-2 rounded-md border border-input bg-card px-3 py-2 text-left text-sm shadow-sm focus:outline-none focus:ring-2 focus:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
        disabled={disabled}
        onClick={() => setOpen((current) => !current)}
        onKeyDown={(event) => {
          if (event.key === "ArrowDown") {
            event.preventDefault();
            setOpen(true);
          }
        }}
      >
        <span className={cn("min-w-0 truncate", selectedOptions.length > 0 ? "text-foreground" : "text-muted-foreground")}>{triggerText}</span>
        <span className="flex shrink-0 items-center gap-1">
          {values.length > 0 ? <span className="rounded-full bg-muted px-1.5 text-[11px] text-muted-foreground">{values.length}</span> : null}
          <ChevronDown className="h-4 w-4 opacity-50" />
        </span>
      </button>
      {open ? createPortal(
        <div ref={panelRef} className="pointer-events-auto fixed z-[70] flex flex-col overflow-hidden rounded-md border bg-popover text-popover-foreground shadow-lg" style={position}>
          <div className="flex shrink-0 items-center gap-2 border-b px-2.5 py-2">
            <Search className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
            <input
              ref={searchRef}
              value={query}
              onChange={(event) => {
                setQuery(event.target.value);
                setActiveIndex(0);
              }}
              className="h-7 min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
              placeholder={searchPlaceholder}
              role="searchbox"
              aria-label={`搜索 ${label}`}
              aria-controls={listboxId}
              aria-activedescendant={itemCount > 0 ? `${listboxId}-option-${activeIndex}` : undefined}
              autoComplete="off"
              onKeyDown={(event) => {
                if (event.key === "Escape") {
                  event.preventDefault();
                  event.stopPropagation();
                  close(true);
                } else if (event.key === "ArrowDown") {
                  event.preventDefault();
                  setActiveIndex((current) => Math.min(current + 1, itemCount - 1));
                } else if (event.key === "ArrowUp") {
                  event.preventDefault();
                  setActiveIndex((current) => Math.max(current - 1, 0));
                } else if (event.key === "Home") {
                  event.preventDefault();
                  setActiveIndex(0);
                } else if (event.key === "End") {
                  event.preventDefault();
                  setActiveIndex(itemCount - 1);
                } else if (event.key === "Enter") {
                  event.preventDefault();
                  toggleOption(activeIndex);
                } else if (event.key === "Tab") {
                  close();
                }
              }}
            />
          </div>
          <div className="flex shrink-0 items-center justify-between gap-2 border-b px-2 py-1.5">
            <button type="button" className="text-xs text-primary underline-offset-4 hover:underline disabled:pointer-events-none disabled:opacity-50" disabled={filteredOptions.length === 0} onClick={selectAllFiltered}>全选当前结果</button>
            <button type="button" className="text-xs text-muted-foreground underline-offset-4 hover:underline disabled:pointer-events-none disabled:opacity-50" disabled={values.length === 0} onClick={() => onChange([])}>清空</button>
          </div>
          <div id={listboxId} role="listbox" aria-label={`${label}选项列表`} aria-multiselectable="true" className="min-h-0 flex-1 overflow-y-auto p-1">
            {filteredOptions.map((option, index) => {
              const selectedOption = values.includes(option.value);
              return (
                <button
                  key={option.value || "__empty__"}
                  id={`${listboxId}-option-${index}`}
                  type="button"
                  role="option"
                  aria-selected={selectedOption}
                  className={cn("relative flex w-full min-w-0 items-center rounded-sm py-1.5 pl-8 pr-2 text-left text-sm outline-none", activeIndex === index ? "bg-accent text-accent-foreground" : "hover:bg-accent/70")}
                  onMouseDown={(event) => event.preventDefault()}
                  onMouseEnter={() => setActiveIndex(index)}
                  onClick={() => toggleOption(index)}
                >
                  <span className="absolute left-2 flex h-3.5 w-3.5 items-center justify-center">{selectedOption ? <Check className="h-4 w-4" /> : null}</span>
                  <span className="block min-w-0 truncate">{option.label}</span>
                </button>
              );
            })}
            {filteredOptions.length === 0 ? <p className="px-3 py-2 text-sm text-muted-foreground">没有匹配项</p> : null}
            {onManualSelect ? (
              <button
                id={`${listboxId}-option-${filteredOptions.length}`}
                type="button"
                role="option"
                aria-selected={false}
                className={cn("flex w-full items-center rounded-sm py-1.5 pl-8 pr-2 text-left text-sm outline-none", activeIndex === filteredOptions.length ? "bg-accent text-accent-foreground" : "hover:bg-accent/70")}
                onMouseDown={(event) => event.preventDefault()}
                onMouseEnter={() => setActiveIndex(filteredOptions.length)}
                onClick={() => toggleOption(filteredOptions.length)}
              >
                手动输入
              </button>
            ) : null}
          </div>
        </div>,
        document.body,
      ) : null}
    </>
  );
}
