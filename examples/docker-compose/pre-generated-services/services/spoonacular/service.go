// Package spoonacular This file is generated ONCE as a starting point and will NOT be overwritten.
// Modify it freely to add your business logic.
// To regenerate, delete this file or set generate.handler.output.overwrite: true in config.
package spoonacular

import (
	"context"

	"github.com/mockzilla/mockzilla/v2/pkg/api"
)

// service implements the ServiceInterface with your business logic.
// Return nil, nil to fall back to the generator for mock responses.
// Return a response to override the generated response.
// Return an error to return an error response.
type service struct {
	params *api.ServiceParams
}

// Ensure service implements ServiceInterface.
var _ ServiceInterface = (*service)(nil)

// newService creates a new service instance.
func newService(params *api.ServiceParams) *service {
	return &service{params: params}
}

// SearchRecipes handles GET /recipes/complexSearch
func (s *service) SearchRecipes(ctx context.Context, opts *SearchRecipesServiceRequestOptions) (*SearchRecipesResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// SearchRecipesByIngredients handles GET /recipes/findByIngredients
func (s *service) SearchRecipesByIngredients(ctx context.Context, opts *SearchRecipesByIngredientsServiceRequestOptions) (*SearchRecipesByIngredientsResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// SearchRecipesByNutrients handles GET /recipes/findByNutrients
func (s *service) SearchRecipesByNutrients(ctx context.Context, opts *SearchRecipesByNutrientsServiceRequestOptions) (*SearchRecipesByNutrientsResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetRecipeInformation handles GET /recipes/{id}/information
func (s *service) GetRecipeInformation(ctx context.Context, opts *GetRecipeInformationServiceRequestOptions) (*GetRecipeInformationResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetRecipeInformationBulk handles GET /recipes/informationBulk
func (s *service) GetRecipeInformationBulk(ctx context.Context, opts *GetRecipeInformationBulkServiceRequestOptions) (*GetRecipeInformationBulkResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetSimilarRecipes handles GET /recipes/{id}/similar
func (s *service) GetSimilarRecipes(ctx context.Context, opts *GetSimilarRecipesServiceRequestOptions) (*GetSimilarRecipesResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetRandomRecipes handles GET /recipes/random
func (s *service) GetRandomRecipes(ctx context.Context, opts *GetRandomRecipesServiceRequestOptions) (*GetRandomRecipesResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// AutocompleteRecipeSearch handles GET /recipes/autocomplete
func (s *service) AutocompleteRecipeSearch(ctx context.Context, opts *AutocompleteRecipeSearchServiceRequestOptions) (*AutocompleteRecipeSearchResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetRecipeTasteByID handles GET /recipes/{id}/tasteWidget.json
func (s *service) GetRecipeTasteByID(ctx context.Context, opts *GetRecipeTasteByIDServiceRequestOptions) (*GetRecipeTasteByIDResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// RecipeTasteByIDImage handles GET /recipes/{id}/tasteWidget.png
func (s *service) RecipeTasteByIDImage(ctx context.Context, opts *RecipeTasteByIDImageServiceRequestOptions) (*RecipeTasteByIDImageResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetRecipeEquipmentByID handles GET /recipes/{id}/equipmentWidget.json
func (s *service) GetRecipeEquipmentByID(ctx context.Context, opts *GetRecipeEquipmentByIDServiceRequestOptions) (*GetRecipeEquipmentByIDResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// EquipmentByIDImage handles GET /recipes/{id}/equipmentWidget.png
func (s *service) EquipmentByIDImage(ctx context.Context, opts *EquipmentByIDImageServiceRequestOptions) (*EquipmentByIDImageResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetRecipePriceBreakdownByID handles GET /recipes/{id}/priceBreakdownWidget.json
func (s *service) GetRecipePriceBreakdownByID(ctx context.Context, opts *GetRecipePriceBreakdownByIDServiceRequestOptions) (*GetRecipePriceBreakdownByIDResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// PriceBreakdownByIDImage handles GET /recipes/{id}/priceBreakdownWidget.png
func (s *service) PriceBreakdownByIDImage(ctx context.Context, opts *PriceBreakdownByIDImageServiceRequestOptions) (*PriceBreakdownByIDImageResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetRecipeIngredientsByID handles GET /recipes/{id}/ingredientWidget.json
func (s *service) GetRecipeIngredientsByID(ctx context.Context, opts *GetRecipeIngredientsByIDServiceRequestOptions) (*GetRecipeIngredientsByIDResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// IngredientsByIDImage handles GET /recipes/{id}/ingredientWidget.png
func (s *service) IngredientsByIDImage(ctx context.Context, opts *IngredientsByIDImageServiceRequestOptions) (*IngredientsByIDImageResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetRecipeNutritionWidgetByID handles GET /recipes/{id}/nutritionWidget.json
func (s *service) GetRecipeNutritionWidgetByID(ctx context.Context, opts *GetRecipeNutritionWidgetByIDServiceRequestOptions) (*GetRecipeNutritionWidgetByIDResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// RecipeNutritionByIDImage handles GET /recipes/{id}/nutritionWidget.png
func (s *service) RecipeNutritionByIDImage(ctx context.Context, opts *RecipeNutritionByIDImageServiceRequestOptions) (*RecipeNutritionByIDImageResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// RecipeNutritionLabelWidget handles GET /recipes/{id}/nutritionLabel
func (s *service) RecipeNutritionLabelWidget(ctx context.Context, opts *RecipeNutritionLabelWidgetServiceRequestOptions) (*RecipeNutritionLabelWidgetResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// RecipeNutritionLabelImage handles GET /recipes/{id}/nutritionLabel.png
func (s *service) RecipeNutritionLabelImage(ctx context.Context, opts *RecipeNutritionLabelImageServiceRequestOptions) (*RecipeNutritionLabelImageResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetAnalyzedRecipeInstructions handles GET /recipes/{id}/analyzedInstructions
func (s *service) GetAnalyzedRecipeInstructions(ctx context.Context, opts *GetAnalyzedRecipeInstructionsServiceRequestOptions) (*GetAnalyzedRecipeInstructionsResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// ExtractRecipeFromWebsite handles GET /recipes/extract
func (s *service) ExtractRecipeFromWebsite(ctx context.Context, opts *ExtractRecipeFromWebsiteServiceRequestOptions) (*ExtractRecipeFromWebsiteResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// VisualizeRecipeIngredientsByID handles GET /recipes/{id}/ingredientWidget
func (s *service) VisualizeRecipeIngredientsByID(ctx context.Context, opts *VisualizeRecipeIngredientsByIDServiceRequestOptions) (*VisualizeRecipeIngredientsByIDResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// VisualizeRecipeTasteByID handles GET /recipes/{id}/tasteWidget
func (s *service) VisualizeRecipeTasteByID(ctx context.Context, opts *VisualizeRecipeTasteByIDServiceRequestOptions) (*VisualizeRecipeTasteByIDResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// VisualizeRecipeEquipmentByID handles GET /recipes/{id}/equipmentWidget
func (s *service) VisualizeRecipeEquipmentByID(ctx context.Context, opts *VisualizeRecipeEquipmentByIDServiceRequestOptions) (*VisualizeRecipeEquipmentByIDResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// VisualizeRecipePriceBreakdownByID handles GET /recipes/{id}/priceBreakdownWidget
func (s *service) VisualizeRecipePriceBreakdownByID(ctx context.Context, opts *VisualizeRecipePriceBreakdownByIDServiceRequestOptions) (*VisualizeRecipePriceBreakdownByIDResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// VisualizeRecipeTaste handles POST /recipes/visualizeTaste
func (s *service) VisualizeRecipeTaste(ctx context.Context, opts *VisualizeRecipeTasteServiceRequestOptions) (*VisualizeRecipeTasteResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// VisualizeRecipeNutrition handles POST /recipes/visualizeNutrition
func (s *service) VisualizeRecipeNutrition(ctx context.Context, opts *VisualizeRecipeNutritionServiceRequestOptions) (*VisualizeRecipeNutritionResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// VisualizePriceBreakdown handles POST /recipes/visualizePriceEstimator
func (s *service) VisualizePriceBreakdown(ctx context.Context, opts *VisualizePriceBreakdownServiceRequestOptions) (*VisualizePriceBreakdownResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// VisualizeEquipment handles POST /recipes/visualizeEquipment
func (s *service) VisualizeEquipment(ctx context.Context, opts *VisualizeEquipmentServiceRequestOptions) (*VisualizeEquipmentResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// AnalyzeRecipe handles POST /recipes/analyze
func (s *service) AnalyzeRecipe(ctx context.Context, opts *AnalyzeRecipeServiceRequestOptions) (*AnalyzeRecipeResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// SummarizeRecipe handles GET /recipes/{id}/summary
func (s *service) SummarizeRecipe(ctx context.Context, opts *SummarizeRecipeServiceRequestOptions) (*SummarizeRecipeResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// CreateRecipeCardGet handles GET /recipes/{id}/card
func (s *service) CreateRecipeCardGet(ctx context.Context, opts *CreateRecipeCardGetServiceRequestOptions) (*CreateRecipeCardGetResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// CreateRecipeCard handles POST /recipes/visualizeRecipe
func (s *service) CreateRecipeCard(ctx context.Context, opts *CreateRecipeCardServiceRequestOptions) (*CreateRecipeCardResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// AnalyzeRecipeInstructions handles POST /recipes/analyzeInstructions
func (s *service) AnalyzeRecipeInstructions(ctx context.Context, opts *AnalyzeRecipeInstructionsServiceRequestOptions) (*AnalyzeRecipeInstructionsResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// ClassifyCuisine handles POST /recipes/cuisine
func (s *service) ClassifyCuisine(ctx context.Context, opts *ClassifyCuisineServiceRequestOptions) (*ClassifyCuisineResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// AnalyzeARecipeSearchQuery handles GET /recipes/queries/analyze
func (s *service) AnalyzeARecipeSearchQuery(ctx context.Context, opts *AnalyzeARecipeSearchQueryServiceRequestOptions) (*AnalyzeARecipeSearchQueryResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// ConvertAmounts handles GET /recipes/convert
func (s *service) ConvertAmounts(ctx context.Context, opts *ConvertAmountsServiceRequestOptions) (*ConvertAmountsResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// ParseIngredients handles POST /recipes/parseIngredients
func (s *service) ParseIngredients(ctx context.Context, opts *ParseIngredientsServiceRequestOptions) (*ParseIngredientsResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// VisualizeRecipeNutritionByID handles GET /recipes/{id}/nutritionWidget
func (s *service) VisualizeRecipeNutritionByID(ctx context.Context, opts *VisualizeRecipeNutritionByIDServiceRequestOptions) (*VisualizeRecipeNutritionByIDResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// VisualizeIngredients handles POST /recipes/visualizeIngredients
func (s *service) VisualizeIngredients(ctx context.Context, opts *VisualizeIngredientsServiceRequestOptions) (*VisualizeIngredientsResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GuessNutritionByDishName handles GET /recipes/guessNutrition
func (s *service) GuessNutritionByDishName(ctx context.Context, opts *GuessNutritionByDishNameServiceRequestOptions) (*GuessNutritionByDishNameResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetIngredientInformation handles GET /food/ingredients/{id}/information
func (s *service) GetIngredientInformation(ctx context.Context, opts *GetIngredientInformationServiceRequestOptions) (*GetIngredientInformationResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// ComputeIngredientAmount handles GET /food/ingredients/{id}/amount
func (s *service) ComputeIngredientAmount(ctx context.Context, opts *ComputeIngredientAmountServiceRequestOptions) (*ComputeIngredientAmountResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// ComputeGlycemicLoad handles POST /food/ingredients/glycemicLoad
func (s *service) ComputeGlycemicLoad(ctx context.Context, opts *ComputeGlycemicLoadServiceRequestOptions) (*ComputeGlycemicLoadResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// AutocompleteIngredientSearch handles GET /food/ingredients/autocomplete
func (s *service) AutocompleteIngredientSearch(ctx context.Context, opts *AutocompleteIngredientSearchServiceRequestOptions) (*AutocompleteIngredientSearchResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// IngredientSearch handles GET /food/ingredients/search
func (s *service) IngredientSearch(ctx context.Context, opts *IngredientSearchServiceRequestOptions) (*IngredientSearchResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetIngredientSubstitutes handles GET /food/ingredients/substitutes
func (s *service) GetIngredientSubstitutes(ctx context.Context, opts *GetIngredientSubstitutesServiceRequestOptions) (*GetIngredientSubstitutesResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetIngredientSubstitutesByID handles GET /food/ingredients/{id}/substitutes
func (s *service) GetIngredientSubstitutesByID(ctx context.Context, opts *GetIngredientSubstitutesByIDServiceRequestOptions) (*GetIngredientSubstitutesByIDResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// SearchGroceryProducts handles GET /food/products/search
func (s *service) SearchGroceryProducts(ctx context.Context, opts *SearchGroceryProductsServiceRequestOptions) (*SearchGroceryProductsResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// SearchGroceryProductsByUPC handles GET /food/products/upc/{upc}
func (s *service) SearchGroceryProductsByUPC(ctx context.Context, opts *SearchGroceryProductsByUPCServiceRequestOptions) (*SearchGroceryProductsByUPCResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// SearchCustomFoods handles GET /food/customFoods/search
func (s *service) SearchCustomFoods(ctx context.Context, opts *SearchCustomFoodsServiceRequestOptions) (*SearchCustomFoodsResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetProductInformation handles GET /food/products/{id}
func (s *service) GetProductInformation(ctx context.Context, opts *GetProductInformationServiceRequestOptions) (*GetProductInformationResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetComparableProducts handles GET /food/products/upc/{upc}/comparable
func (s *service) GetComparableProducts(ctx context.Context, opts *GetComparableProductsServiceRequestOptions) (*GetComparableProductsResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// AutocompleteProductSearch handles GET /food/products/suggest
func (s *service) AutocompleteProductSearch(ctx context.Context, opts *AutocompleteProductSearchServiceRequestOptions) (*AutocompleteProductSearchResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// VisualizeProductNutritionByID handles GET /food/products/{id}/nutritionWidget
func (s *service) VisualizeProductNutritionByID(ctx context.Context, opts *VisualizeProductNutritionByIDServiceRequestOptions) (*VisualizeProductNutritionByIDResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// ProductNutritionByIDImage handles GET /food/products/{id}/nutritionWidget.png
func (s *service) ProductNutritionByIDImage(ctx context.Context, opts *ProductNutritionByIDImageServiceRequestOptions) (*ProductNutritionByIDImageResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// ProductNutritionLabelWidget handles GET /food/products/{id}/nutritionLabel
func (s *service) ProductNutritionLabelWidget(ctx context.Context, opts *ProductNutritionLabelWidgetServiceRequestOptions) (*ProductNutritionLabelWidgetResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// ProductNutritionLabelImage handles GET /food/products/{id}/nutritionLabel.png
func (s *service) ProductNutritionLabelImage(ctx context.Context, opts *ProductNutritionLabelImageServiceRequestOptions) (*ProductNutritionLabelImageResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// ClassifyGroceryProduct handles POST /food/products/classify
func (s *service) ClassifyGroceryProduct(ctx context.Context, opts *ClassifyGroceryProductServiceRequestOptions) (*ClassifyGroceryProductResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// ClassifyGroceryProductBulk handles POST /food/products/classifyBatch
func (s *service) ClassifyGroceryProductBulk(ctx context.Context, opts *ClassifyGroceryProductBulkServiceRequestOptions) (*ClassifyGroceryProductBulkResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// MapIngredientsToGroceryProducts handles POST /food/ingredients/map
func (s *service) MapIngredientsToGroceryProducts(ctx context.Context, opts *MapIngredientsToGroceryProductsServiceRequestOptions) (*MapIngredientsToGroceryProductsResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// AutocompleteMenuItemSearch handles GET /food/menuItems/suggest
func (s *service) AutocompleteMenuItemSearch(ctx context.Context, opts *AutocompleteMenuItemSearchServiceRequestOptions) (*AutocompleteMenuItemSearchResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// SearchMenuItems handles GET /food/menuItems/search
func (s *service) SearchMenuItems(ctx context.Context, opts *SearchMenuItemsServiceRequestOptions) (*SearchMenuItemsResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetMenuItemInformation handles GET /food/menuItems/{id}
func (s *service) GetMenuItemInformation(ctx context.Context, opts *GetMenuItemInformationServiceRequestOptions) (*GetMenuItemInformationResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// VisualizeMenuItemNutritionByID handles GET /food/menuItems/{id}/nutritionWidget
func (s *service) VisualizeMenuItemNutritionByID(ctx context.Context, opts *VisualizeMenuItemNutritionByIDServiceRequestOptions) (*VisualizeMenuItemNutritionByIDResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// MenuItemNutritionByIDImage handles GET /food/menuItems/{id}/nutritionWidget.png
func (s *service) MenuItemNutritionByIDImage(ctx context.Context, opts *MenuItemNutritionByIDImageServiceRequestOptions) (*MenuItemNutritionByIDImageResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// MenuItemNutritionLabelWidget handles GET /food/menuItems/{id}/nutritionLabel
func (s *service) MenuItemNutritionLabelWidget(ctx context.Context, opts *MenuItemNutritionLabelWidgetServiceRequestOptions) (*MenuItemNutritionLabelWidgetResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// MenuItemNutritionLabelImage handles GET /food/menuItems/{id}/nutritionLabel.png
func (s *service) MenuItemNutritionLabelImage(ctx context.Context, opts *MenuItemNutritionLabelImageServiceRequestOptions) (*MenuItemNutritionLabelImageResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GenerateMealPlan handles GET /mealplanner/generate
func (s *service) GenerateMealPlan(ctx context.Context, opts *GenerateMealPlanServiceRequestOptions) (*GenerateMealPlanResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetMealPlanWeek handles GET /mealplanner/{username}/week/{start-date}
func (s *service) GetMealPlanWeek(ctx context.Context, opts *GetMealPlanWeekServiceRequestOptions) (*GetMealPlanWeekResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// ClearMealPlanDay handles DELETE /mealplanner/{username}/day/{date}
func (s *service) ClearMealPlanDay(ctx context.Context, opts *ClearMealPlanDayServiceRequestOptions) (*ClearMealPlanDayResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// AddToMealPlan handles POST /mealplanner/{username}/items
func (s *service) AddToMealPlan(ctx context.Context, opts *AddToMealPlanServiceRequestOptions) (*AddToMealPlanResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// DeleteFromMealPlan handles DELETE /mealplanner/{username}/items/{id}
func (s *service) DeleteFromMealPlan(ctx context.Context, opts *DeleteFromMealPlanServiceRequestOptions) (*DeleteFromMealPlanResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetMealPlanTemplates handles GET /mealplanner/{username}/templates
func (s *service) GetMealPlanTemplates(ctx context.Context, opts *GetMealPlanTemplatesServiceRequestOptions) (*GetMealPlanTemplatesResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// AddMealPlanTemplate handles POST /mealplanner/{username}/templates
func (s *service) AddMealPlanTemplate(ctx context.Context, opts *AddMealPlanTemplateServiceRequestOptions) (*AddMealPlanTemplateResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetMealPlanTemplate handles GET /mealplanner/{username}/templates/{id}
func (s *service) GetMealPlanTemplate(ctx context.Context, opts *GetMealPlanTemplateServiceRequestOptions) (*GetMealPlanTemplateResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// DeleteMealPlanTemplate handles DELETE /mealplanner/{username}/templates/{id}
func (s *service) DeleteMealPlanTemplate(ctx context.Context, opts *DeleteMealPlanTemplateServiceRequestOptions) (*DeleteMealPlanTemplateResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetShoppingList handles GET /mealplanner/{username}/shopping-list
func (s *service) GetShoppingList(ctx context.Context, opts *GetShoppingListServiceRequestOptions) (*GetShoppingListResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GenerateShoppingList handles POST /mealplanner/{username}/shopping-list/{start-date}/{end-date}
func (s *service) GenerateShoppingList(ctx context.Context, opts *GenerateShoppingListServiceRequestOptions) (*GenerateShoppingListResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// ConnectUser handles POST /users/connect
func (s *service) ConnectUser(ctx context.Context, opts *ConnectUserServiceRequestOptions) (*ConnectUserResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// AddToShoppingList handles POST /mealplanner/{username}/shopping-list/items
func (s *service) AddToShoppingList(ctx context.Context, opts *AddToShoppingListServiceRequestOptions) (*AddToShoppingListResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// DeleteFromShoppingList handles DELETE /mealplanner/{username}/shopping-list/items/{id}
func (s *service) DeleteFromShoppingList(ctx context.Context, opts *DeleteFromShoppingListServiceRequestOptions) (*DeleteFromShoppingListResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// SearchRestaurants handles GET /food/restaurants/search
func (s *service) SearchRestaurants(ctx context.Context, opts *SearchRestaurantsServiceRequestOptions) (*SearchRestaurantsResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetDishPairingForWine handles GET /food/wine/dishes
func (s *service) GetDishPairingForWine(ctx context.Context, opts *GetDishPairingForWineServiceRequestOptions) (*GetDishPairingForWineResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetWinePairing handles GET /food/wine/pairing
func (s *service) GetWinePairing(ctx context.Context, opts *GetWinePairingServiceRequestOptions) (*GetWinePairingResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetWineDescription handles GET /food/wine/description
func (s *service) GetWineDescription(ctx context.Context, opts *GetWineDescriptionServiceRequestOptions) (*GetWineDescriptionResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetWineRecommendation handles GET /food/wine/recommendation
func (s *service) GetWineRecommendation(ctx context.Context, opts *GetWineRecommendationServiceRequestOptions) (*GetWineRecommendationResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// ImageClassificationByURL handles GET /food/images/classify
func (s *service) ImageClassificationByURL(ctx context.Context, opts *ImageClassificationByURLServiceRequestOptions) (*ImageClassificationByURLResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// ImageAnalysisByURL handles GET /food/images/analyze
func (s *service) ImageAnalysisByURL(ctx context.Context, opts *ImageAnalysisByURLServiceRequestOptions) (*ImageAnalysisByURLResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// QuickAnswer handles GET /recipes/quickAnswer
func (s *service) QuickAnswer(ctx context.Context, opts *QuickAnswerServiceRequestOptions) (*QuickAnswerResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// DetectFoodInText handles POST /food/detect
func (s *service) DetectFoodInText(ctx context.Context, opts *DetectFoodInTextServiceRequestOptions) (*DetectFoodInTextResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// SearchSiteContent handles GET /food/site/search
func (s *service) SearchSiteContent(ctx context.Context, opts *SearchSiteContentServiceRequestOptions) (*SearchSiteContentResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// SearchAllFood handles GET /food/search
func (s *service) SearchAllFood(ctx context.Context, opts *SearchAllFoodServiceRequestOptions) (*SearchAllFoodResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// SearchFoodVideos handles GET /food/videos/search
func (s *service) SearchFoodVideos(ctx context.Context, opts *SearchFoodVideosServiceRequestOptions) (*SearchFoodVideosResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetARandomFoodJoke handles GET /food/jokes/random
func (s *service) GetARandomFoodJoke(ctx context.Context) (*GetARandomFoodJokeResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetRandomFoodTrivia handles GET /food/trivia/random
func (s *service) GetRandomFoodTrivia(ctx context.Context) (*GetRandomFoodTriviaResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// TalkToChatbot handles GET /food/converse
func (s *service) TalkToChatbot(ctx context.Context, opts *TalkToChatbotServiceRequestOptions) (*TalkToChatbotResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetConversationSuggests handles GET /food/converse/suggest
func (s *service) GetConversationSuggests(ctx context.Context, opts *GetConversationSuggestsServiceRequestOptions) (*GetConversationSuggestsResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}
