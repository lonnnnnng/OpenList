import { Center } from "@hope-ui/solid"
import { createMemo, Show } from "solid-js"
import { useT } from "~/hooks"
import { DriverItem, Type } from "~/types"
import { Item, ItemProps } from "./Item"

type Value = string | number | boolean

// long: 驱动JSON只声明宽Type，按类型缩窄值后交给组件，保留全局ItemProps判别联合。
export const SchemaItem = (props: {
  item: DriverItem
  driver: string
  value: unknown
  onChange: (value: Value) => void
}) => {
  const t = useT()
  const adapted = createMemo<ItemProps | undefined>(() => {
    const common = { ...props.item, driver: props.driver, onChange: props.onChange }
    switch (props.item.type) {
      case Type.Bool:
        return { ...common, type: Type.Bool, value: typeof props.value === "boolean" ? props.value : false }
      case Type.Number:
        return { ...common, type: Type.Number, value: typeof props.value === "number" ? props.value : 0 }
      case Type.Float:
        return { ...common, type: Type.Float, value: typeof props.value === "number" ? props.value : 0 }
      case Type.String:
      case Type.Text:
        return { ...common, type: props.item.type, value: typeof props.value === "string" ? props.value : "" }
      case Type.Select:
        return { ...common, type: Type.Select, value: typeof props.value === "string" ? props.value : "" }
      default:
        return undefined
    }
  })
  return (
    <Show when={adapted()} fallback={<Center>{t("settings.unknown_type")}</Center>}>
      {(value) => <Item {...value()} />}
    </Show>
  )
}
