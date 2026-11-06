package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	apishop "github.com/kaecyra/gobbler/internal/api/shopping"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/quantity"
)

type listSummary struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type createListIn struct {
	Name string `json:"name" jsonschema:"the new list's name"`
}

type getListIn struct {
	ID int64 `json:"id" jsonschema:"the list's id from list_shopping_lists"`
}

type itemOut struct {
	Aisle string `json:"aisle"`
	Name  string `json:"name"`
	// Amounts are the merged amounts to buy, such as "3 cups"; empty for an
	// item with no amount.
	Amounts []string `json:"amounts,omitempty"`
	// Key identifies the item for check_item.
	Key     string `json:"key"`
	Checked bool   `json:"checked"`
}

type listOut struct {
	ID      int64        `json:"id"`
	Name    string       `json:"name"`
	Recipes []listRecipe `json:"recipes"`
	Items   []itemOut    `json:"items"`
}

type listRecipe struct {
	RecipeID int64  `json:"recipe_id"`
	Name     string `json:"name"`
	Scale    string `json:"scale"`
}

type addRecipeIn struct {
	ListID   int64  `json:"list_id"`
	RecipeID int64  `json:"recipe_id"`
	Scale    string `json:"scale,omitempty" jsonschema:"how much to multiply the recipe by, such as 2 or 1/2 or 1.5; default 1"`
}

type addManualIn struct {
	ListID int64  `json:"list_id"`
	Text   string `json:"text" jsonschema:"the item as written, such as 'paper towels' or '2 lemons'"`
}

type checkItemIn struct {
	ListID  int64  `json:"list_id"`
	Key     string `json:"key" jsonschema:"the item's key from get_shopping_list"`
	Checked bool   `json:"checked" jsonschema:"true to tick the item off, false to untick it"`
}

type changeOut struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

type pantryOut struct {
	Ingredients []string `json:"ingredients"`
}

// shopCaller is the shopping service's Caller for a tool call: the credential's
// owner and the household the membership check admitted it to.
func shopCaller(c Credential, h household.Household) apishop.Caller {
	return apishop.Caller{Person: c.Email, Household: h}
}

func (t *tools) registerShopping(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "list_shopping_lists", Description: "List the household's shopping lists. Returns one page and a cursor for the next.", Annotations: readOnly}, t.listLists)
	mcp.AddTool(s, &mcp.Tool{Name: "get_shopping_list", Description: "Get one shopping list: its recipes and its merged items grouped by aisle, with what is ticked off.", Annotations: readOnly}, t.getList)
	mcp.AddTool(s, &mcp.Tool{Name: "create_shopping_list", Description: "Make a new, empty shopping list. Applies at once.", Annotations: applies}, t.createList)
	mcp.AddTool(s, &mcp.Tool{Name: "add_recipe_to_shopping_list", Description: "Add a recipe, scaled, to a shopping list. Its ingredients merge into the list. Applies at once.", Annotations: applies}, t.addRecipe)
	mcp.AddTool(s, &mcp.Tool{Name: "add_manual_shopping_item", Description: "Add a hand-written item to a shopping list. Applies at once.", Annotations: applies}, t.addManual)
	mcp.AddTool(s, &mcp.Tool{Name: "check_shopping_item", Description: "Tick an item of a shopping list off, or untick it. Applies at once.", Annotations: applies}, t.checkItem)
	mcp.AddTool(s, &mcp.Tool{Name: "get_pantry", Description: "Read the pantry: the ingredients the household always has, which shopping lists leave out.", Annotations: readOnly}, t.getPantry)
}

func (t *tools) listLists(ctx context.Context, _ *mcp.CallToolRequest, in listIn) (*mcp.CallToolResult, Page[listSummary], error) {
	c, h, err := scoped(ctx)
	if err != nil {
		return nil, Page[listSummary]{}, t.toolError(ctx, "list_shopping_lists", err)
	}
	caller := shopCaller(c, h)
	lists, err := t.shop.Lists(ctx, caller)
	if err != nil {
		return nil, Page[listSummary]{}, t.toolError(ctx, "list_shopping_lists", err)
	}
	all := make([]listSummary, len(lists))
	for i, l := range lists {
		all[i] = listSummary{ID: l.ID, Name: l.Name}
	}
	p, err := paginate(all, in.Cursor, in.Limit)
	if err != nil {
		return nil, Page[listSummary]{}, refuse("%s", err)
	}
	return nil, p, nil
}

func (t *tools) getList(ctx context.Context, _ *mcp.CallToolRequest, in getListIn) (*mcp.CallToolResult, listOut, error) {
	c, h, err := scoped(ctx)
	if err != nil {
		return nil, listOut{}, t.toolError(ctx, "get_shopping_list", err)
	}
	caller := shopCaller(c, h)
	v, err := t.shop.Load(ctx, caller, in.ID)
	if err != nil {
		return nil, listOut{}, t.toolError(ctx, "get_shopping_list", err)
	}
	out := listOut{ID: v.List.ID, Name: v.List.Name, Recipes: []listRecipe{}, Items: []itemOut{}}
	for _, e := range v.Entries {
		out.Recipes = append(out.Recipes, listRecipe{RecipeID: e.RecipeID, Name: e.Name, Scale: e.Multiplier.String()})
	}
	for _, a := range v.Aisles {
		for _, it := range a.Items {
			io := itemOut{Aisle: a.Name, Name: it.Name, Key: string(it.Key), Checked: it.Checked}
			for _, am := range it.Amounts {
				r, err := quantity.Round(am)
				if err != nil {
					return nil, listOut{}, t.toolError(ctx, "get_shopping_list", fmt.Errorf("round %q: %w", it.Name, err))
				}
				io.Amounts = append(io.Amounts, r.Rounded.String())
			}
			out.Items = append(out.Items, io)
		}
	}
	return nil, out, nil
}

func (t *tools) createList(ctx context.Context, _ *mcp.CallToolRequest, in createListIn) (*mcp.CallToolResult, listSummary, error) {
	const tool = "create_shopping_list"
	c, h, err := scoped(ctx)
	if err != nil {
		return nil, listSummary{}, t.toolError(ctx, tool, err)
	}
	caller := shopCaller(c, h)
	l, err := t.shop.CreateList(ctx, caller, in.Name)
	if err != nil {
		return nil, listSummary{}, t.toolError(ctx, tool, err)
	}
	if err := t.recordWrite(ctx, c, h.ID, tool, fmt.Sprintf("list %d", l.ID)); err != nil {
		return nil, listSummary{}, t.toolError(ctx, tool, err)
	}
	return nil, listSummary{ID: l.ID, Name: l.Name}, nil
}

func (t *tools) addRecipe(ctx context.Context, _ *mcp.CallToolRequest, in addRecipeIn) (*mcp.CallToolResult, changeOut, error) {
	const tool = "add_recipe_to_shopping_list"
	c, h, err := scoped(ctx)
	if err != nil {
		return nil, changeOut{}, t.toolError(ctx, tool, err)
	}
	caller := shopCaller(c, h)
	scale := quantity.Int(1)
	if s := strings.TrimSpace(in.Scale); s != "" {
		if scale, err = quantity.ParseRat(s); err != nil || scale.Sign() <= 0 {
			return nil, changeOut{}, refuse("scale must be a positive number such as 2, 1/2 or 1.5")
		}
	}
	if _, err := t.shop.AddEntry(ctx, caller, in.ListID, in.RecipeID, scale); err != nil {
		return nil, changeOut{}, t.toolError(ctx, tool, err)
	}
	if err := t.recordWrite(ctx, c, h.ID, tool, fmt.Sprintf("list %d recipe %d", in.ListID, in.RecipeID)); err != nil {
		return nil, changeOut{}, t.toolError(ctx, tool, err)
	}
	return nil, changeOut{OK: true, Message: fmt.Sprintf("added recipe %d, scaled by %s, to list %d", in.RecipeID, scale, in.ListID)}, nil
}

func (t *tools) addManual(ctx context.Context, _ *mcp.CallToolRequest, in addManualIn) (*mcp.CallToolResult, changeOut, error) {
	const tool = "add_manual_shopping_item"
	c, h, err := scoped(ctx)
	if err != nil {
		return nil, changeOut{}, t.toolError(ctx, tool, err)
	}
	caller := shopCaller(c, h)
	id, err := t.shop.AddManualItem(ctx, caller, in.ListID, apishop.ManualItem{Text: in.Text})
	if err != nil {
		return nil, changeOut{}, t.toolError(ctx, tool, err)
	}
	if err := t.recordWrite(ctx, c, h.ID, tool, fmt.Sprintf("list %d item %d", in.ListID, id)); err != nil {
		return nil, changeOut{}, t.toolError(ctx, tool, err)
	}
	return nil, changeOut{OK: true, Message: fmt.Sprintf("added %q to list %d", strings.TrimSpace(in.Text), in.ListID)}, nil
}

func (t *tools) checkItem(ctx context.Context, _ *mcp.CallToolRequest, in checkItemIn) (*mcp.CallToolResult, changeOut, error) {
	const tool = "check_shopping_item"
	c, h, err := scoped(ctx)
	if err != nil {
		return nil, changeOut{}, t.toolError(ctx, tool, err)
	}
	caller := shopCaller(c, h)
	if err := t.shop.SetChecked(ctx, caller, in.ListID, apishop.ItemKey(in.Key), in.Checked); err != nil {
		return nil, changeOut{}, t.toolError(ctx, tool, err)
	}
	if err := t.recordWrite(ctx, c, h.ID, tool, fmt.Sprintf("list %d item %s", in.ListID, in.Key)); err != nil {
		return nil, changeOut{}, t.toolError(ctx, tool, err)
	}
	return nil, changeOut{OK: true, Message: fmt.Sprintf("item %s on list %d checked=%t", in.Key, in.ListID, in.Checked)}, nil
}

func (t *tools) getPantry(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, pantryOut, error) {
	c, h, err := scoped(ctx)
	if err != nil {
		return nil, pantryOut{}, t.toolError(ctx, "get_pantry", err)
	}
	caller := shopCaller(c, h)
	ids, err := t.shop.Pantry(ctx, caller)
	if err != nil {
		return nil, pantryOut{}, t.toolError(ctx, "get_pantry", err)
	}
	out := pantryOut{Ingredients: []string{}}
	for _, id := range ids {
		ing, err := t.shop.Ingredient(ctx, caller, int64(id))
		if err != nil {
			return nil, pantryOut{}, t.toolError(ctx, "get_pantry", fmt.Errorf("pantry ingredient %d: %w", id, err))
		}
		out.Ingredients = append(out.Ingredients, ing.Name)
	}
	return nil, out, nil
}
