package design_test

import (
	"errors"

	. "github.com/goadesign/goa/design"
	. "github.com/goadesign/goa/design/apidsl"
	"github.com/goadesign/goa/dslengine"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Project", func() {
	var mt *MediaTypeDefinition
	var view string

	var projected *MediaTypeDefinition
	var links *UserTypeDefinition
	var prErr error

	JustBeforeEach(func() {
		projected, links, prErr = mt.Project(view)
	})

	Context("with a media type with a default and a tiny view", func() {
		BeforeEach(func() {
			mt = &MediaTypeDefinition{
				UserTypeDefinition: &UserTypeDefinition{
					AttributeDefinition: &AttributeDefinition{
						Type: Object{
							"att1": &AttributeDefinition{Type: Integer},
							"att2": &AttributeDefinition{Type: String},
						},
					},
					TypeName: "Foo",
				},
				Identifier: "vnd.application/foo",
				Views: map[string]*ViewDefinition{
					"default": {
						Name: "default",
						AttributeDefinition: &AttributeDefinition{
							Type: Object{
								"att1": &AttributeDefinition{Type: String},
								"att2": &AttributeDefinition{Type: String},
							},
						},
					},
					"tiny": {
						Name: "tiny",
						AttributeDefinition: &AttributeDefinition{
							Type: Object{
								"att2": &AttributeDefinition{Type: String},
							},
						},
					},
				},
			}
		})

		Context("using the empty view", func() {
			BeforeEach(func() {
				view = ""
			})

			It("returns an error", func() {
				Ω(prErr).Should(HaveOccurred())
			})
		})

		Context("using the default view", func() {
			BeforeEach(func() {
				view = "default"
			})

			It("returns a media type with the default view attributes", func() {
				Ω(prErr).ShouldNot(HaveOccurred())
				Ω(projected).ShouldNot(BeNil())
				Ω(projected.Type).Should(BeAssignableToTypeOf(Object{}))
				Ω(projected.Type.ToObject()).Should(HaveKey("att1"))
				att := projected.Type.ToObject()["att1"]
				Ω(att).ShouldNot(BeNil())
				Ω(att.Type).ShouldNot(BeNil())
				Ω(att.Type.Kind()).Should(Equal(IntegerKind))
			})
		})

		Context("using the tiny view", func() {
			BeforeEach(func() {
				view = "tiny"
			})

			It("returns a media type with the default view attributes", func() {
				Ω(prErr).ShouldNot(HaveOccurred())
				Ω(projected).ShouldNot(BeNil())
				Ω(projected.Type).Should(BeAssignableToTypeOf(Object{}))
				Ω(projected.Type.ToObject()).Should(HaveKey("att2"))
				att := projected.Type.ToObject()["att2"]
				Ω(att).ShouldNot(BeNil())
				Ω(att.Type).ShouldNot(BeNil())
				Ω(att.Type.Kind()).Should(Equal(StringKind))
			})
		})

	})

	Context("with media types with view attributes with a cyclical dependency", func() {
		const id = "vnd.application/MT1"
		const typeName = "Mt1"

		BeforeEach(func() {
			dslengine.Reset()
			API("test", func() {})
			mt = MediaType(id, func() {
				TypeName(typeName)
				Attributes(func() {
					Attribute("att", "vnd.application/MT2")
				})
				Links(func() {
					Link("att", "default")
				})
				View("default", func() {
					Attribute("att")
					Attribute("links")
				})
			})
			MediaType("vnd.application/MT2", func() {
				TypeName("Mt2")
				Attributes(func() {
					Attribute("att2", mt)
				})
				Links(func() {
					Link("att2", "default")
				})
				View("default", func() {
					Attribute("att2")
					Attribute("links")
				})
			})
			err := dslengine.Run()
			Ω(err).ShouldNot(HaveOccurred())
			Ω(dslengine.Errors).ShouldNot(HaveOccurred())
		})

		Context("using the default view", func() {
			BeforeEach(func() {
				view = "default"
			})

			It("returns the projected media type with links", func() {
				Ω(prErr).ShouldNot(HaveOccurred())
				Ω(projected).ShouldNot(BeNil())
				Ω(projected.Type).Should(BeAssignableToTypeOf(Object{}))
				Ω(projected.Type.ToObject()).Should(HaveKey("att"))
				l := projected.Type.ToObject()["links"]
				Ω(l.Type.(*UserTypeDefinition).AttributeDefinition).Should(Equal(links.AttributeDefinition))
				Ω(links.Type.ToObject()).Should(HaveKey("att"))
			})
		})
	})
})

var _ = Describe("UserTypes", func() {
	var (
		o         Object
		userTypes map[string]*UserTypeDefinition
	)

	JustBeforeEach(func() {
		userTypes = UserTypes(o)
	})

	Context("with an object not using user types", func() {
		BeforeEach(func() {
			o = Object{"foo": &AttributeDefinition{Type: String}}
		})

		It("returns nil", func() {
			Ω(userTypes).Should(BeNil())
		})
	})

	Context("with an object with an attribute using a user type", func() {
		var ut *UserTypeDefinition
		BeforeEach(func() {
			ut = &UserTypeDefinition{
				TypeName:            "foo",
				AttributeDefinition: &AttributeDefinition{Type: String},
			}

			o = Object{"foo": &AttributeDefinition{Type: ut}}
		})

		It("returns the user type", func() {
			Ω(userTypes).Should(HaveLen(1))
			Ω(userTypes[ut.TypeName]).Should(Equal(ut))
		})
	})

	Context("with an object with an attribute using recursive user types", func() {
		var ut, childut *UserTypeDefinition

		BeforeEach(func() {
			childut = &UserTypeDefinition{
				TypeName:            "child",
				AttributeDefinition: &AttributeDefinition{Type: String},
			}
			child := Object{"child": &AttributeDefinition{Type: childut}}
			ut = &UserTypeDefinition{
				TypeName:            "parent",
				AttributeDefinition: &AttributeDefinition{Type: child},
			}

			o = Object{"foo": &AttributeDefinition{Type: ut}}
		})

		It("returns the user types", func() {
			Ω(userTypes).Should(HaveLen(2))
			Ω(userTypes[ut.TypeName]).Should(Equal(ut))
			Ω(userTypes[childut.TypeName]).Should(Equal(childut))
		})
	})
})

var _ = Describe("MediaTypeDefinition", func() {
	Describe("IterateViews", func() {
		var (
			m  *MediaTypeDefinition
			it ViewIterator

			iteratedViews []string
		)
		BeforeEach(func() {
			m = &MediaTypeDefinition{}

			// setup iterator that just accumulates view names into iteratedViews
			iteratedViews = []string{}
			it = func(v *ViewDefinition) error {
				iteratedViews = append(iteratedViews, v.Name)
				return nil
			}
		})
		It("works with empty", func() {
			Expect(m.Views).To(BeEmpty())
			Expect(m.IterateViews(it)).To(Succeed())
			Expect(iteratedViews).To(BeEmpty())
		})
		Context("with non-empty views map", func() {
			BeforeEach(func() {
				m.Views = map[string]*ViewDefinition{
					"d": {Name: "d"},
					"c": {Name: "c"},
					"a": {Name: "a"},
					"b": {Name: "b"},
				}
			})
			It("sorts views", func() {
				Expect(m.IterateViews(it)).To(Succeed())
				Expect(iteratedViews).To(Equal([]string{"a", "b", "c", "d"}))
			})
			It("propagates error", func() {
				errIterator := func(v *ViewDefinition) error {
					if len(iteratedViews) > 2 {
						return errors.New("foo")
					}
					iteratedViews = append(iteratedViews, v.Name)
					return nil
				}
				Expect(m.IterateViews(errIterator)).To(MatchError("foo"))
				Expect(iteratedViews).To(Equal([]string{"a", "b", "c"}))
			})
		})
	})
})

var _ = Describe("Walk", func() {
	var target DataStructure
	var matchedName string
	var count int
	var matched bool

	counter := func(*AttributeDefinition) {
		count++
	}

	matcher := func(name string) func(*AttributeDefinition) {
		return func(att *AttributeDefinition) {
			if u, ok := att.Type.(*UserTypeDefinition); ok {
				if u.TypeName == name {
					matched = true
				}
			} else if m, ok := att.Type.(*MediaTypeDefinition); ok {
				if m.TypeName == name {
					matched = true
				}
			}
		}
	}

	BeforeEach(func() {
		matchedName = ""
		count = 0
		matched = false
	})

	JustBeforeEach(func() {
		target.Walk(counter)
		if matchedName != "" {
			target.Walk(matcher(matchedName))
		}
	})

	Context("with simple attribute", func() {
		BeforeEach(func() {
			target = &AttributeDefinition{Type: String}
		})

		It("walks it", func() {
			Ω(count).Should(Equal(1))
		})
	})

	Context("with an object attribute", func() {
		BeforeEach(func() {
			o := Object{"foo": &AttributeDefinition{Type: String}}
			target = &AttributeDefinition{Type: o}
		})

		It("walks it", func() {
			Ω(count).Should(Equal(2))
		})
	})

	Context("with an object attribute containing user types", func() {
		const typeName = "foo"
		BeforeEach(func() {
			matchedName = typeName
			at := &AttributeDefinition{Type: String}
			ut := &UserTypeDefinition{AttributeDefinition: at, TypeName: typeName}
			o := Object{"foo": &AttributeDefinition{Type: ut}}
			target = &AttributeDefinition{Type: o}
		})

		It("walks it", func() {
			Ω(count).Should(Equal(3))
			Ω(matched).Should(BeTrue())
		})
	})

	Context("with an object attribute containing recursive user types", func() {
		const typeName = "foo"
		BeforeEach(func() {
			matchedName = typeName
			co := Object{}
			at := &AttributeDefinition{Type: co}
			ut := &UserTypeDefinition{AttributeDefinition: at, TypeName: typeName}
			co["recurse"] = &AttributeDefinition{Type: ut}
			o := Object{"foo": &AttributeDefinition{Type: ut}}
			target = &AttributeDefinition{Type: o}
		})

		It("walks it", func() {
			Ω(count).Should(Equal(4))
			Ω(matched).Should(BeTrue())
		})
	})
})
