import { ComponentFixture, TestBed } from '@angular/core/testing';
import { CrudMaker } from './crud-maker';

describe('CrudMaker', () => {
  let component: CrudMaker;
  let fixture: ComponentFixture<CrudMaker>;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [CrudMaker],
    }).compileComponents();

    fixture = TestBed.createComponent(CrudMaker);
    component = fixture.componentInstance;
    await fixture.whenStable();
  });

  it('should create', () => {
    expect(component).toBeTruthy();
  });
});
